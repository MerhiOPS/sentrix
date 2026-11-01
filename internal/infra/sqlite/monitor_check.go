package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/uptime"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// RecordCheck implements uptime.StateRepo: everything one result changes,
// in one writer transaction, the events before the state (R1). A monitor
// that was paused, edited or deleted while it was checked is not written.
func (s *Store) RecordCheck(ctx context.Context, sc domain.Scope, rec uptime.CheckRecord) ([]int64, error) {
	var ids []int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		tk, err := tenantKey(ctx, tx, sc)
		if err != nil {
			return err
		}
		mk := rec.Monitor.ID
		var rev int64
		var paused int
		err = tx.QueryRowContext(ctx, `SELECT revision, paused FROM monitors WHERE tenant_key = ? AND key = ?`, tk, mk).
			Scan(&rev, &paused)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && (rev != rec.Revision || paused == 1)) {
			return uptime.ErrStale
		}
		if err != nil {
			return fmt.Errorf("read monitor revision: %w", err)
		}
		if err := insertResult(ctx, tx, tk, mk, rec.Result); err != nil {
			return err
		}
		if err := rollupCheck(ctx, tx, tk, mk, rec.Result, rec.Step); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO usage_daily (tenant_key, day, checks) VALUES (?, ?, 1)
			ON CONFLICT (tenant_key, day) DO UPDATE SET checks = checks + 1`, tk, utcDay(rec.Result.At)); err != nil {
			return fmt.Errorf("meter check: %w", err)
		}
		if err := applyIncident(ctx, tx, tk, mk, rec.Step); err != nil {
			return err
		}
		if ids, err = insertMonitorEvents(ctx, tx, tk, mk, rec.Step.Events); err != nil {
			return err
		}
		return writeMonitorState(ctx, tx, tk, mk, rec.Step.Next)
	})
	return ids, err
}

func insertResult(ctx context.Context, tx *sql.Tx, tk, mk int64, r domain.CheckResult) error {
	var latency, ttfb, status, loss any
	if r.Outcome != domain.CheckUnknown {
		latency = r.Latency.Milliseconds()
	}
	if r.TTFB > 0 {
		ttfb = r.TTFB.Milliseconds()
	}
	if r.StatusCode > 0 {
		status = r.StatusCode
	}
	if r.Loss > 0 {
		loss = r.Loss
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO monitor_results (tenant_key, monitor_key, at, outcome, class, latency_ms,
		ttfb_ms, status, loss, cert_expiry) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tk, mk, encTime(r.At), string(r.Outcome), string(r.Class), latency, ttfb, status, loss, encOptTime(r.CertExpiry))
	if err != nil {
		return fmt.Errorf("insert check result: %w", err)
	}
	return nil
}

// hourDelta is one change to an hourly rollup row.
type hourDelta struct {
	hour             time.Time
	checks, failures int
	up, down         time.Duration
	latSum, latMax   time.Duration
	latN             int
}

// rollupCheck attributes the covered and reclassified time to their hours
// and counts the check in the hour it ran.
func rollupCheck(ctx context.Context, tx *sql.Tx, tk, mk int64, r domain.CheckResult, st domain.MonitorStep) error {
	var deltas []hourDelta
	for _, sl := range domain.HourSlices(st.Covered) {
		d := hourDelta{hour: sl.Hour, up: sl.D}
		if st.CoveredDown {
			d.up, d.down = 0, sl.D
		}
		deltas = append(deltas, d)
	}
	if st.Reclassify != nil {
		for _, sl := range domain.HourSlices(*st.Reclassify) {
			deltas = append(deltas, hourDelta{hour: sl.Hour, up: -sl.D, down: sl.D})
		}
	}
	d := hourDelta{hour: r.At.UTC().Truncate(time.Hour), checks: 1}
	if r.Outcome == domain.CheckDown {
		d.failures = 1
	}
	if r.Outcome != domain.CheckUnknown && r.Latency > 0 {
		d.latSum, d.latMax, d.latN = r.Latency, r.Latency, 1
	}
	deltas = append(deltas, d)
	for _, d := range deltas {
		// up_ms is clamped at zero: a reclassified span can include time
		// that was never counted as up (an unknown result inside it).
		if _, err := tx.ExecContext(ctx, `INSERT INTO monitor_hourly (tenant_key, monitor_key, hour, checks, failures,
			up_ms, down_ms, latency_sum_ms, latency_n, latency_max_ms) VALUES (?, ?, ?, ?, ?, max(?, 0), ?, ?, ?, ?)
			ON CONFLICT (tenant_key, monitor_key, hour) DO UPDATE SET checks = checks + excluded.checks,
				failures = failures + excluded.failures, up_ms = max(up_ms + ?, 0), down_ms = down_ms + excluded.down_ms,
				latency_sum_ms = latency_sum_ms + excluded.latency_sum_ms, latency_n = latency_n + excluded.latency_n,
				latency_max_ms = max(latency_max_ms, excluded.latency_max_ms)`,
			tk, mk, encTime(d.hour), d.checks, d.failures, d.up.Milliseconds(), d.down.Milliseconds(),
			d.latSum.Milliseconds(), d.latN, d.latMax.Milliseconds(), d.up.Milliseconds()); err != nil {
			return fmt.Errorf("roll up check: %w", err)
		}
	}
	return nil
}

func applyIncident(ctx context.Context, tx *sql.Tx, tk, mk int64, st domain.MonitorStep) error {
	if o := st.Open; o != nil {
		if _, err := tx.ExecContext(ctx, `INSERT INTO monitor_incidents (tenant_key, monitor_key, started, class, failures)
			VALUES (?, ?, ?, ?, ?)`, tk, mk, encTime(o.Start), string(o.Class), o.Failures); err != nil {
			return fmt.Errorf("open incident: %w", err)
		}
	}
	if st.Failure {
		if _, err := tx.ExecContext(ctx, `UPDATE monitor_incidents SET failures = failures + 1
			WHERE tenant_key = ? AND monitor_key = ? AND ended IS NULL`, tk, mk); err != nil {
			return fmt.Errorf("count incident failure: %w", err)
		}
	}
	if st.Close != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE monitor_incidents SET ended = ?
			WHERE tenant_key = ? AND monitor_key = ? AND ended IS NULL`, encTime(*st.Close), tk, mk); err != nil {
			return fmt.Errorf("close incident: %w", err)
		}
	}
	return nil
}

func insertMonitorEvents(ctx context.Context, tx *sql.Tx, tk, mk int64, evs []domain.MonitorEvent) ([]int64, error) {
	ids := make([]int64, 0, len(evs))
	for _, ev := range evs {
		var downFor any
		if ev.Kind == domain.EventUp {
			downFor = ev.DownFor.Milliseconds()
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO monitor_events (tenant_key, monitor_key, kind, at, class, down_for_ms,
			cert_expiry, notify) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, tk, mk, string(ev.Kind), encTime(ev.At), string(ev.Class),
			downFor, encOptTime(ev.CertExpiry), boolInt(ev.Notify))
		if err != nil {
			return nil, fmt.Errorf("insert monitor event: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func writeMonitorState(ctx context.Context, tx *sql.Tx, tk, mk int64, st domain.MonitorState) error {
	var firstFailure any
	if !st.FirstFailure.IsZero() {
		firstFailure = encTime(st.FirstFailure)
	}
	_, err := tx.ExecContext(ctx, `UPDATE monitors SET status = ?, since = ?, last_check = ?, failures = ?,
		first_failure = ?, cert = ?, cert_expiry = ?, revision = revision + 1 WHERE tenant_key = ? AND key = ?`,
		string(st.Status), encTime(st.Since), encTime(st.LastCheck), st.Failures, firstFailure, string(st.Cert),
		encOptTime(st.CertExpiry), tk, mk)
	if err != nil {
		return fmt.Errorf("write monitor state: %w", err)
	}
	return nil
}
