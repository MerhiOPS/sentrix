package retention

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// MonitorTarget is one monitor of one tenant: monitor data is pruned per
// monitor, the way host data is pruned per host.
type MonitorTarget struct {
	Tenant  domain.TenantID
	Monitor int64
	Plan    domain.Plan
}

// MonitorStore is what the janitor needs for uptime data. Cross-tenant, so
// every method takes an OpsScope.
type MonitorStore interface {
	MonitorRetentionTargets(ctx context.Context, ops domain.OpsScope) ([]MonitorTarget, error)
	// PruneMonitor deletes at most limit rows of kind older than before.
	// For incidents, "older" means ended before (an open one is kept).
	PruneMonitor(ctx context.Context, ops domain.OpsScope, t MonitorTarget, k domain.DataKind, before time.Time, limit int) (int, error)
	OldestMonitor(ctx context.Context, ops domain.OpsScope, t MonitorTarget, k domain.DataKind) (time.Time, bool, error)
}

// SetMonitorStore makes RunOnce prune uptime data too. Call before running.
func (j *Janitor) SetMonitorStore(m MonitorStore) { j.monitors = m }

// pruneMonitors is RunOnce's pass over uptime data; it returns the rows
// deleted and the worst overdue age, like the host pass.
func (j *Janitor) pruneMonitors(ctx context.Context, ops domain.OpsScope, now time.Time) (int, time.Duration, error) {
	if j.monitors == nil {
		return 0, 0, nil
	}
	targets, err := j.monitors.MonitorRetentionTargets(ctx, ops)
	if err != nil {
		return 0, 0, fmt.Errorf("list monitor retention targets: %w", err)
	}
	var errs []error
	var deleted int
	var overdue time.Duration
	for _, t := range targets {
		ret := j.ret
		if t.Plan.MetricsRetentionDays > 0 {
			ret = t.Plan.Retention(j.ret)
		}
		for _, k := range domain.MonitorDataKinds {
			keep, ok := ret[k]
			if !ok {
				continue
			}
			cutoff := now.Add(-keep)
			for {
				n, err := j.monitors.PruneMonitor(ctx, ops, t, k, cutoff, BatchRows)
				deleted += n
				if err != nil {
					errs = append(errs, fmt.Errorf("prune %s for monitor %d: %w", k, t.Monitor, err))
					break
				}
				if n < BatchRows || j.pause(ctx, batchPause) != nil {
					break
				}
			}
			oldest, any, err := j.monitors.OldestMonitor(ctx, ops, t, k)
			if err == nil && any && oldest.Before(cutoff) {
				overdue = max(overdue, cutoff.Sub(oldest))
			}
		}
	}
	return deleted, overdue, errors.Join(errs...)
}
