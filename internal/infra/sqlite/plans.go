package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// SetPlan implements identity.PlanRepo.
func (s *Store) SetPlan(ctx context.Context, ops domain.OpsScope, t domain.TenantID, p domain.Plan) error {
	if _, err := ops.Reason(); err != nil {
		return err
	}
	res, err := s.db.Writer().ExecContext(ctx, `UPDATE tenants SET max_hosts = ?, log_bytes_per_day = ?,
		metrics_retention_days = ?, logs_retention_days = ?, max_monitors = ?, min_check_interval_ms = ? WHERE id = ?`,
		p.MaxHosts, p.LogBytesPerDay, p.MetricsRetentionDays, p.LogsRetentionDays, p.MaxMonitors,
		p.MinCheckInterval.Milliseconds(), encID(domain.ID(t)))
	if err != nil {
		return fmt.Errorf("set plan: %w", err)
	}
	return mustAffect(res, "tenant")
}

// GetPlan implements identity.PlanRepo.
func (s *Store) GetPlan(ctx context.Context, sc domain.Scope) (domain.Plan, error) {
	t, err := sc.Tenant()
	if err != nil {
		return domain.Plan{}, err
	}
	p, err := scanPlan(s.db.Reader().QueryRowContext(ctx, `SELECT `+planColumns+` FROM tenants WHERE id = ?`,
		encID(domain.ID(t))).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Plan{}, fmt.Errorf("tenant: %w", domain.ErrNotFound)
	}
	return p, err
}

// planColumns are a tenant row's plan, in scanPlan's order.
const planColumns = `max_hosts, log_bytes_per_day, metrics_retention_days, logs_retention_days, max_monitors, min_check_interval_ms`

func scanPlan(scan func(...any) error, before ...any) (domain.Plan, error) {
	var p domain.Plan
	var minCheck int64
	err := scan(append(before, &p.MaxHosts, &p.LogBytesPerDay, &p.MetricsRetentionDays, &p.LogsRetentionDays,
		&p.MaxMonitors, &minCheck)...)
	p.MinCheckInterval = time.Duration(minCheck) * time.Millisecond
	return p, err
}

// GetUsage implements identity.PlanRepo.
func (s *Store) GetUsage(ctx context.Context, sc domain.Scope, now time.Time) (domain.Usage, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return domain.Usage{}, err
	}
	var u domain.Usage
	today := utcDay(now)
	monthStart := utcDay(time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC))
	if err := r.QueryRowContext(ctx, `SELECT count(*) FROM hosts WHERE tenant_key = ? AND disabled_at IS NULL`, tk).Scan(&u.Hosts); err != nil {
		return u, fmt.Errorf("count hosts: %w", err)
	}
	if err := r.QueryRowContext(ctx, `SELECT coalesce(sum(bytes), 0) FROM log_usage WHERE tenant_key = ? AND day = ?`,
		tk, today).Scan(&u.LogBytesToday); err != nil {
		return u, fmt.Errorf("log usage today: %w", err)
	}
	err = r.QueryRowContext(ctx, `SELECT coalesce(sum(samples), 0), coalesce(sum(log_bytes), 0), coalesce(max(hosts), 0),
		coalesce(sum(checks), 0) FROM usage_daily WHERE tenant_key = ? AND day >= ? AND day <= ?`, tk, monthStart, today).
		Scan(&u.MonthSamples, &u.MonthLogBytes, &u.MonthPeakHosts, &u.MonthChecks)
	if err != nil {
		return u, fmt.Errorf("month usage: %w", err)
	}
	if err := r.QueryRowContext(ctx, `SELECT count(*) FROM monitors WHERE tenant_key = ?`, tk).Scan(&u.Monitors); err != nil {
		return u, fmt.Errorf("count monitors: %w", err)
	}
	return u, nil
}

// meterBatch records what a batch stored, inside its transaction and after
// the replay gate: a resent batch never reaches here, so it is never
// counted twice.
func meterBatch(ctx context.Context, tx *batchTx, tk, hk, day int64, samples int, logBytes int64) error {
	res, err := tx.ExecContext(ctx, `INSERT INTO usage_host_days (tenant_key, day, host_key) VALUES (?, ?, ?)
		ON CONFLICT DO NOTHING`, tk, day, hk)
	if err != nil {
		return fmt.Errorf("meter host day: %w", err)
	}
	firstToday, err := res.RowsAffected()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO usage_daily (tenant_key, day, samples, log_bytes, hosts) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (tenant_key, day) DO UPDATE SET samples = samples + excluded.samples,
			log_bytes = log_bytes + excluded.log_bytes, hosts = hosts + excluded.hosts`,
		tk, day, samples, logBytes, firstToday)
	if err != nil {
		return fmt.Errorf("meter batch: %w", err)
	}
	return nil
}

// hostsAtLimit reports whether a tenant already has max_hosts enabled hosts.
func hostsAtLimit(ctx context.Context, q rowQuerier, tk int64) (bool, error) {
	var limit, n int
	if err := q.QueryRowContext(ctx, `SELECT max_hosts FROM tenants WHERE key = ?`, tk).Scan(&limit); err != nil {
		return false, fmt.Errorf("read max_hosts: %w", err)
	}
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM hosts WHERE tenant_key = ? AND disabled_at IS NULL`, tk).Scan(&n); err != nil {
		return false, fmt.Errorf("count hosts: %w", err)
	}
	return n >= limit, nil
}
