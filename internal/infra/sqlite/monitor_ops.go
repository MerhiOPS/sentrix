package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/retention"
	"github.com/MerhiOPS/sentrix/internal/app/uptime"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// ScheduledMonitors implements uptime.ScheduleReader: every unpaused monitor
// of every tenant, with its state and channels. One of the deliberately
// unscoped reads: it runs under an OpsScope.
func (s *Store) ScheduledMonitors(ctx context.Context, ops domain.OpsScope) ([]uptime.Scheduled, error) {
	if _, err := ops.Reason(); err != nil {
		return nil, err
	}
	r := s.db.Reader()
	rows, err := r.QueryContext(ctx, `SELECT t.id, m.tenant_key, `+scheduledColumns+`
		FROM monitors m JOIN tenants t ON t.key = m.tenant_key WHERE m.paused = 0 ORDER BY m.key`)
	if err != nil {
		return nil, fmt.Errorf("list scheduled monitors: %w", err)
	}
	defer rows.Close()
	var out []uptime.Scheduled
	tenantKeys := []int64{}
	for rows.Next() {
		var raw []byte
		var tk int64
		var mr monitorRow
		var sr stateRow
		if err := rows.Scan(append(append([]any{&raw, &tk}, mr.dest()...), sr.dest()...)...); err != nil {
			return nil, fmt.Errorf("scan scheduled monitor: %w", err)
		}
		m, err := mr.monitor()
		if err != nil {
			return nil, err
		}
		id, err := decID(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, uptime.Scheduled{Tenant: domain.TenantID(id), Monitor: m, State: sr.state()})
		tenantKeys = append(tenantKeys, tk)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, scheduledChannels(ctx, r, out, tenantKeys)
}

// scheduledChannels fills channels per tenant, through the tenant-scoped
// read the dashboard uses.
func scheduledChannels(ctx context.Context, r *sql.DB, out []uptime.Scheduled, tenantKeys []int64) error {
	groups := map[int64][]int{}
	for i, tk := range tenantKeys {
		groups[tk] = append(groups[tk], i)
	}
	for tk, idx := range groups {
		ms := make([]domain.Monitor, len(idx))
		for j, i := range idx {
			ms[j] = out[i].Monitor
		}
		if err := monitorChannels(ctx, r, tk, ms); err != nil {
			return err
		}
		for j, i := range idx {
			out[i].Monitor = ms[j]
		}
	}
	return nil
}

// scheduledColumns are monitorColumns then stateColumns, qualified by m.
// TestScheduledColumnsMatch keeps them in step.
const scheduledColumns = `m.key, m.name, m.kind, m.target, m.interval_ms, m.timeout_ms, m.confirm_retries, m.method,
	m.accept_status, m.keyword, m.keyword_mode, m.keyword_case, m.cert_warn_days, m.paused, m.created_at,
	m.key, m.status, m.since, m.last_check, m.failures, m.first_failure, m.cert, m.cert_expiry, m.revision`

// targetPlanColumns are planColumns qualified by t.
const targetPlanColumns = `t.max_hosts, t.log_bytes_per_day, t.metrics_retention_days, t.logs_retention_days,
	t.max_monitors, t.min_check_interval_ms`

// FailUndeliveredMonitorEvents implements uptime.Sweeper.
func (s *Store) FailUndeliveredMonitorEvents(ctx context.Context, ops domain.OpsScope, reason string) (int, error) {
	if _, err := ops.Reason(); err != nil {
		return 0, err
	}
	res, err := s.db.Writer().ExecContext(ctx, `UPDATE monitor_events SET outcome = 'failed', delivery = ?
		WHERE notify = 1 AND outcome = ''`, reason)
	if err != nil {
		return 0, fmt.Errorf("sweep undelivered monitor events: %w", err)
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// pruneMonitorSQL deletes one bounded batch of a kind for one (tenant,
// monitor), oldest first. Arguments: tenant, monitor, cutoff, limit.
// Incidents and events share a kind: an incident is pruned once it ended
// before the cutoff, never while open.
var pruneMonitorSQL = map[domain.DataKind][]string{
	domain.DataMonitorResults: {`DELETE FROM monitor_results WHERE tenant_key = ?1 AND monitor_key = ?2 AND at IN
		(SELECT at FROM monitor_results WHERE tenant_key = ?1 AND monitor_key = ?2 AND at < ?3 ORDER BY at LIMIT ?4)`},
	domain.DataMonitorHourly: {`DELETE FROM monitor_hourly WHERE tenant_key = ?1 AND monitor_key = ?2 AND hour IN
		(SELECT hour FROM monitor_hourly WHERE tenant_key = ?1 AND monitor_key = ?2 AND hour < ?3 ORDER BY hour LIMIT ?4)`},
	domain.DataMonitorIncidents: {
		`DELETE FROM monitor_incidents WHERE tenant_key = ?1 AND key IN (SELECT key FROM monitor_incidents
		WHERE tenant_key = ?1 AND monitor_key = ?2 AND ended IS NOT NULL AND ended < ?3 ORDER BY started LIMIT ?4)`,
		`DELETE FROM monitor_events WHERE tenant_key = ?1 AND key IN (SELECT key FROM monitor_events
		WHERE tenant_key = ?1 AND monitor_key = ?2 AND at < ?3 ORDER BY key LIMIT ?4)`,
	},
}

var oldestMonitorSQL = map[domain.DataKind]string{
	domain.DataMonitorResults: `SELECT MIN(at) FROM monitor_results WHERE tenant_key = ?1 AND monitor_key = ?2`,
	domain.DataMonitorHourly:  `SELECT MIN(hour) FROM monitor_hourly WHERE tenant_key = ?1 AND monitor_key = ?2`,
	domain.DataMonitorIncidents: `SELECT MIN(t) FROM (SELECT MIN(ended) AS t FROM monitor_incidents
		WHERE tenant_key = ?1 AND monitor_key = ?2 UNION ALL SELECT MIN(at) FROM monitor_events
		WHERE tenant_key = ?1 AND monitor_key = ?2)`,
}

// MonitorRetentionTargets implements retention.MonitorStore: every monitor
// of every tenant, under an OpsScope.
func (s *Store) MonitorRetentionTargets(ctx context.Context, ops domain.OpsScope) ([]retention.MonitorTarget, error) {
	if _, err := ops.Reason(); err != nil {
		return nil, err
	}
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT t.id, m.key, `+targetPlanColumns+`
		FROM monitors m JOIN tenants t ON t.key = m.tenant_key ORDER BY m.key`)
	if err != nil {
		return nil, fmt.Errorf("list monitor retention targets: %w", err)
	}
	defer rows.Close()
	var out []retention.MonitorTarget
	for rows.Next() {
		var raw []byte
		var mk int64
		p, err := scanPlan(rows.Scan, &raw, &mk)
		if err != nil {
			return nil, fmt.Errorf("scan monitor retention target: %w", err)
		}
		id, err := decID(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, retention.MonitorTarget{Tenant: domain.TenantID(id), Monitor: mk, Plan: p})
	}
	return out, rows.Err()
}

// PruneMonitor implements retention.MonitorStore.
func (s *Store) PruneMonitor(ctx context.Context, ops domain.OpsScope, t retention.MonitorTarget, k domain.DataKind,
	before time.Time, limit int) (int, error) {
	stmts, ok := pruneMonitorSQL[k]
	if !ok {
		return 0, fmt.Errorf("%w: data kind %q", domain.ErrInvalid, k)
	}
	w := s.db.Writer()
	tk, err := monitorTargetKey(ctx, w, ops, t)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, stmt := range stmts {
		res, err := w.ExecContext(ctx, stmt, tk, t.Monitor, encTime(before), limit)
		if err != nil {
			return total, fmt.Errorf("prune %s: %w", k, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += int(n)
	}
	return total, nil
}

// OldestMonitor implements retention.MonitorStore.
func (s *Store) OldestMonitor(ctx context.Context, ops domain.OpsScope, t retention.MonitorTarget, k domain.DataKind) (time.Time, bool, error) {
	stmt, ok := oldestMonitorSQL[k]
	if !ok {
		return time.Time{}, false, fmt.Errorf("%w: data kind %q", domain.ErrInvalid, k)
	}
	r := s.db.Reader()
	tk, err := monitorTargetKey(ctx, r, ops, t)
	if err != nil {
		return time.Time{}, false, err
	}
	var oldest sql.NullInt64
	if err := r.QueryRowContext(ctx, stmt, tk, t.Monitor).Scan(&oldest); err != nil {
		return time.Time{}, false, fmt.Errorf("oldest %s: %w", k, err)
	}
	return decTime(oldest.Int64), oldest.Valid, nil
}

func monitorTargetKey(ctx context.Context, q rowQuerier, ops domain.OpsScope, t retention.MonitorTarget) (int64, error) {
	if _, err := ops.Reason(); err != nil {
		return 0, err
	}
	sc, err := domain.NewScope(t.Tenant)
	if err != nil {
		return 0, err
	}
	return tenantKey(ctx, q, sc)
}
