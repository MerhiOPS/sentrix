package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

const monitorColumns = `key, name, kind, target, interval_ms, timeout_ms, confirm_retries, method, accept_status,
	keyword, keyword_mode, keyword_case, cert_warn_days, paused, created_at`

// ListMonitors implements uptime.MonitorRepo, ordered by id.
func (s *Store) ListMonitors(ctx context.Context, sc domain.Scope) ([]domain.Monitor, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	rows, err := r.QueryContext(ctx, `SELECT `+monitorColumns+` FROM monitors WHERE tenant_key = ? ORDER BY key`, tk)
	if err != nil {
		return nil, fmt.Errorf("list monitors: %w", err)
	}
	defer rows.Close()
	var out []domain.Monitor
	for rows.Next() {
		m, err := scanMonitor(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, monitorChannels(ctx, r, tk, out)
}

// GetMonitor implements uptime.MonitorRepo.
func (s *Store) GetMonitor(ctx context.Context, sc domain.Scope, id int64) (domain.Monitor, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return domain.Monitor{}, err
	}
	m, err := scanMonitor(r.QueryRowContext(ctx, `SELECT `+monitorColumns+` FROM monitors WHERE tenant_key = ? AND key = ?`,
		tk, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return m, fmt.Errorf("monitor: %w", domain.ErrNotFound)
	}
	if err != nil {
		return m, err
	}
	out := []domain.Monitor{m}
	err = monitorChannels(ctx, r, tk, out)
	return out[0], err
}

// monitorRow is one monitors row's definition columns, in monitorColumns
// order: dest for Scan, then monitor to convert.
type monitorRow struct {
	m                                     domain.Monitor
	kind, accept, mode                    string
	interval, timeout, created, kcase, ps int64
}

func (r *monitorRow) dest() []any {
	return []any{&r.m.ID, &r.m.Name, &r.kind, &r.m.Target, &r.interval, &r.timeout, &r.m.ConfirmRetries, &r.m.Method,
		&r.accept, &r.m.Keyword, &r.mode, &r.kcase, &r.m.CertWarnDays, &r.ps, &r.created}
}

func (r *monitorRow) monitor() (domain.Monitor, error) {
	m := r.m
	m.Kind, m.KeywordMode, m.KeywordCase, m.Paused = domain.MonitorKind(r.kind), domain.KeywordMode(r.mode), r.kcase == 1, r.ps == 1
	m.Interval, m.Timeout = time.Duration(r.interval)*time.Millisecond, time.Duration(r.timeout)*time.Millisecond
	m.CreatedAt = decTime(r.created)
	if r.accept != "" {
		var err error
		if m.AcceptStatus, err = domain.ParseStatusRanges(r.accept); err != nil {
			return m, fmt.Errorf("stored status ranges: %w", err)
		}
	}
	return m, nil
}

func scanMonitor(scan func(...any) error) (domain.Monitor, error) {
	var r monitorRow
	if err := scan(r.dest()...); err != nil {
		return domain.Monitor{}, fmt.Errorf("scan monitor: %w", err)
	}
	return r.monitor()
}

// monitorChannels fills each monitor's channels.
func monitorChannels(ctx context.Context, r *sql.DB, tk int64, ms []domain.Monitor) error {
	index := map[int64]int{}
	for i, m := range ms {
		index[m.ID] = i
	}
	rows, err := r.QueryContext(ctx, `SELECT monitor_key, channel_key FROM monitor_channels
		WHERE tenant_key = ? ORDER BY monitor_key, channel_key`, tk)
	if err != nil {
		return fmt.Errorf("read monitor channels: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var mk, ck int64
		if err := rows.Scan(&mk, &ck); err != nil {
			return fmt.Errorf("scan monitor channel: %w", err)
		}
		if i, ok := index[mk]; ok {
			ms[i].Channels = append(ms[i].Channels, ck)
		}
	}
	return rows.Err()
}

func monitorValues(m domain.Monitor) []any {
	return []any{m.Name, string(m.Kind), m.Target, m.Interval.Milliseconds(), m.Timeout.Milliseconds(), m.ConfirmRetries,
		m.Method, domain.FormatStatusRanges(m.AcceptStatus), m.Keyword, string(m.KeywordMode), boolInt(m.KeywordCase), m.CertWarnDays}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// CreateMonitor implements uptime.MonitorRepo. The count and the insert
// share the writer transaction, so two concurrent creations cannot both
// take the last slot.
func (s *Store) CreateMonitor(ctx context.Context, sc domain.Scope, m domain.Monitor, maxMonitors int) (int64, error) {
	var id int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		tk, err := tenantKey(ctx, tx, sc)
		if err != nil {
			return err
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM monitors WHERE tenant_key = ?`, tk).Scan(&n); err != nil {
			return fmt.Errorf("count monitors: %w", err)
		}
		if n >= maxMonitors {
			return fmt.Errorf("%w: the plan allows %d monitors", domain.ErrQuotaExceeded, maxMonitors)
		}
		now := encTime(m.CreatedAt)
		res, err := tx.ExecContext(ctx, `INSERT INTO monitors (tenant_key, name, kind, target, interval_ms, timeout_ms,
			confirm_retries, method, accept_status, keyword, keyword_mode, keyword_case, cert_warn_days, paused, created_at,
			status, since) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, 'pending', ?)`,
			append(append([]any{tk}, monitorValues(m)...), now, now)...)
		if err != nil {
			return fmt.Errorf("insert monitor: %w", err)
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		return linkMonitor(ctx, tx, tk, id, m.Channels)
	})
	return id, err
}

// UpdateMonitor implements uptime.MonitorRepo.
func (s *Store) UpdateMonitor(ctx context.Context, sc domain.Scope, m domain.Monitor, at time.Time) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		tk, err := tenantKey(ctx, tx, sc)
		if err != nil {
			return err
		}
		var kind, target string
		var paused int
		err = tx.QueryRowContext(ctx, `SELECT kind, target, paused FROM monitors WHERE tenant_key = ? AND key = ?`, tk, m.ID).
			Scan(&kind, &target, &paused)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("monitor: %w", domain.ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("read monitor: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE monitors SET name = ?, kind = ?, target = ?, interval_ms = ?, timeout_ms = ?,
			confirm_retries = ?, method = ?, accept_status = ?, keyword = ?, keyword_mode = ?, keyword_case = ?,
			cert_warn_days = ?, revision = revision + 1 WHERE tenant_key = ? AND key = ?`,
			append(monitorValues(m), tk, m.ID)...); err != nil {
			return fmt.Errorf("update monitor: %w", err)
		}
		if (kind != string(m.Kind) || target != m.Target) && paused == 0 {
			if err := resetMonitor(ctx, tx, tk, m.ID, "pending", at); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM monitor_channels WHERE tenant_key = ? AND monitor_key = ?`, tk, m.ID); err != nil {
			return fmt.Errorf("unlink monitor: %w", err)
		}
		return linkMonitor(ctx, tx, tk, m.ID, m.Channels)
	})
}

// DeleteMonitor implements uptime.MonitorRepo; history cascades.
func (s *Store) DeleteMonitor(ctx context.Context, sc domain.Scope, id int64) error {
	return s.deleteOwned(ctx, sc, `DELETE FROM monitors WHERE tenant_key = ? AND key = ?`, id, "monitor")
}

// SetPaused implements uptime.MonitorRepo.
func (s *Store) SetPaused(ctx context.Context, sc domain.Scope, id int64, paused bool, at time.Time) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		tk, err := tenantKey(ctx, tx, sc)
		if err != nil {
			return err
		}
		var cur int
		err = tx.QueryRowContext(ctx, `SELECT paused FROM monitors WHERE tenant_key = ? AND key = ?`, tk, id).Scan(&cur)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("monitor: %w", domain.ErrNotFound)
		}
		if err != nil || (cur == 1) == paused {
			return err
		}
		status := "pending"
		if paused {
			status = "paused"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE monitors SET paused = ? WHERE tenant_key = ? AND key = ?`,
			boolInt(paused), tk, id); err != nil {
			return fmt.Errorf("set paused: %w", err)
		}
		return resetMonitor(ctx, tx, tk, id, status, at)
	})
}

// resetMonitor starts the state over (pending or paused) and ends any open
// incident at at: what was observed no longer describes the monitor.
func resetMonitor(ctx context.Context, tx *sql.Tx, tk, id int64, status string, at time.Time) error {
	if _, err := tx.ExecContext(ctx, `UPDATE monitor_incidents SET ended = ? WHERE tenant_key = ? AND monitor_key = ?
		AND ended IS NULL`, encTime(at), tk, id); err != nil {
		return fmt.Errorf("end incident: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE monitors SET status = ?, since = ?, failures = 0, first_failure = NULL,
		revision = revision + 1 WHERE tenant_key = ? AND key = ?`, status, encTime(at), tk, id); err != nil {
		return fmt.Errorf("reset monitor state: %w", err)
	}
	return nil
}

// linkMonitor stores a monitor's channels, each resolved inside the tenant.
func linkMonitor(ctx context.Context, tx *sql.Tx, tk, id int64, channels []int64) error {
	for _, c := range channels {
		if err := channelExists(ctx, tx, tk, c); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO monitor_channels (tenant_key, monitor_key, channel_key)
			VALUES (?, ?, ?)`, tk, id, c); err != nil {
			return fmt.Errorf("link monitor channel: %w", err)
		}
	}
	return nil
}
