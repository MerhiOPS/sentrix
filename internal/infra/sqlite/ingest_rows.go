package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

func loadCounters(ctx context.Context, tx *sql.Tx, tk, hk int64) (ingest.CounterState, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT counter, last_raw, offset FROM host_counters WHERE tenant_key = ? AND host_key = ?`, tk, hk)
	if err != nil {
		return nil, fmt.Errorf("load counters: %w", err)
	}
	defer rows.Close()
	state := ingest.CounterState{}
	for rows.Next() {
		var name string
		var last, off int64
		if err := rows.Scan(&name, &last, &off); err != nil {
			return nil, fmt.Errorf("scan counter: %w", err)
		}
		var cv ingest.CounterValue
		if err := decInto([]int64{last, off}, []*uint64{&cv.LastRaw, &cv.Offset}); err != nil {
			return nil, err
		}
		state[ingest.Counter(name)] = cv
	}
	return state, rows.Err()
}

// writePrepared writes everything a batch produces except its seq record.
// Only samples actually inserted reach the rollups: a conflicting duplicate
// is counted, never aggregated twice.
func writePrepared(ctx context.Context, tx *sql.Tx, tk, hk int64, p ingest.Prepared, sr *seriesResolver) (ingest.CommitResult, error) {
	var res ingest.CommitResult
	for _, smp := range p.Samples {
		inserted, err := insertCore(ctx, tx, `INSERT OR IGNORE INTO metrics_core`, tk, hk, smp)
		if err != nil {
			return res, err
		}
		if !inserted {
			res.Conflicts++
			continue
		}
		res.Stored++
		if err := rollupSample(ctx, tx, tk, hk, smp); err != nil {
			return res, err
		}
	}
	if err := writeDetails(ctx, sr, p.Details, &res); err != nil {
		return res, err
	}
	if err := writeSnapshots(ctx, tx, tk, hk, p.Processes, &res); err != nil {
		return res, err
	}
	if n := len(p.Samples); n > 0 {
		if err := upsertLatest(ctx, tx, tk, hk, p.Samples[n-1]); err != nil {
			return res, err
		}
	}
	if err := saveCounters(ctx, tx, tk, hk, p.Counters); err != nil {
		return res, err
	}
	return res, touchHost(ctx, tx, tk, hk, p, res.Conflicts)
}

// coreArgs flattens a sample into the metrics_core/latest_core column order.
func coreArgs(tk, hk int64, s domain.CoreSample) ([]any, error) {
	args := []any{tk, hk, encTime(s.TS),
		encGauge(s.CPUPercent), encGauge(s.MemoryPercent), encGauge(s.DiskPercent),
		encGauge(s.Load1), encGauge(s.Load5), encGauge(s.Load15)}
	for _, v := range []uint64{s.MemoryUsed, s.UptimeSeconds, s.NetRxBytes, s.NetTxBytes, s.DiskReadBytes, s.DiskWriteBytes} {
		i, err := encU64(v)
		if err != nil {
			return nil, err
		}
		args = append(args, i)
	}
	return args, nil
}

const coreColumns = `(tenant_key, host_key, ts, cpu, mem, disk, load1, load5, load15,
	mem_used, uptime, net_rx, net_tx, disk_r, disk_w) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

func insertCore(ctx context.Context, tx *sql.Tx, verb string, tk, hk int64, s domain.CoreSample) (bool, error) {
	args, err := coreArgs(tk, hk, s)
	if err != nil {
		return false, err
	}
	r, err := tx.ExecContext(ctx, verb+" "+coreColumns, args...)
	if err != nil {
		return false, fmt.Errorf("insert sample: %w", err)
	}
	n, err := r.RowsAffected()
	return n == 1, err
}

// upsertLatest keeps the newest sample per host. The WHERE guard means an
// older batch (a late replay) cannot move the fleet grid backwards.
func upsertLatest(ctx context.Context, tx *sql.Tx, tk, hk int64, s domain.CoreSample) error {
	args, err := coreArgs(tk, hk, s)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO latest_core `+coreColumns+`
		ON CONFLICT (host_key) DO UPDATE SET
			ts = excluded.ts, cpu = excluded.cpu, mem = excluded.mem, disk = excluded.disk,
			load1 = excluded.load1, load5 = excluded.load5, load15 = excluded.load15,
			mem_used = excluded.mem_used, uptime = excluded.uptime,
			net_rx = excluded.net_rx, net_tx = excluded.net_tx,
			disk_r = excluded.disk_r, disk_w = excluded.disk_w
		WHERE excluded.ts > latest_core.ts AND latest_core.tenant_key = excluded.tenant_key`, args...)
	if err != nil {
		return fmt.Errorf("upsert latest: %w", err)
	}
	return nil
}

func saveCounters(ctx context.Context, tx *sql.Tx, tk, hk int64, st ingest.CounterState) error {
	for name, cv := range st {
		last, err := encU64(cv.LastRaw)
		if err != nil {
			return err
		}
		off, err := encU64(cv.Offset)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO host_counters (tenant_key, host_key, counter, last_raw, offset)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (host_key, counter) DO UPDATE SET last_raw = excluded.last_raw, offset = excluded.offset
			WHERE host_counters.tenant_key = excluded.tenant_key`,
			tk, hk, string(name), last, off); err != nil {
			return fmt.Errorf("save counter %s: %w", name, err)
		}
	}
	return nil
}

// touchHost records liveness and the agent's self-report.
func touchHost(ctx context.Context, tx *sql.Tx, tk, hk int64, p ingest.Prepared, conflicts int) error {
	dropped, err := encU64(p.Agent.DroppedTotal)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE hosts SET last_seen = ?, interval_ms = ?,
		agent_version = coalesce(nullif(?, ''), agent_version),
		buffer_depth = ?, dropped_total = ?, clock_skew_ms = ?,
		rejected_total = rejected_total + ?, conflict_total = conflict_total + ?
		WHERE tenant_key = ? AND key = ?`,
		encTime(p.ReceivedAt), p.Interval.Milliseconds(), truncate(p.Agent.Version, 100),
		p.Agent.BufferDepth, dropped, p.Agent.ClockSkew.Milliseconds(),
		p.Rejected, conflicts, tk, hk)
	if err != nil {
		return fmt.Errorf("touch host: %w", err)
	}
	return nil
}

// truncate cuts to n bytes without leaving half a UTF-8 rune.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}
