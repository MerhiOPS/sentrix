// Package retention deletes data past its retention, in bounded batches
// through the single writer, and reports how far behind it is.
package retention

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Target is one host of one tenant: pruning is per (tenant, host), so every
// delete walks a primary-key prefix instead of scanning a whole table.
type Target struct {
	Tenant domain.TenantID
	Host   domain.HostID
}

// Store is what the janitor needs. It is cross-tenant by nature, so every
// method takes an OpsScope.
type Store interface {
	RetentionTargets(ctx context.Context, ops domain.OpsScope) ([]Target, error)
	// PruneHost deletes at most limit rows of kind older than before and
	// returns how many it deleted.
	PruneHost(ctx context.Context, ops domain.OpsScope, t Target, k domain.DataKind, before time.Time, limit int) (int, error)
	// OldestHost returns the oldest timestamp of kind, false if there is none.
	OldestHost(ctx context.Context, ops domain.OpsScope, t Target, k domain.DataKind) (time.Time, bool, error)
	// Compact returns freed pages to the filesystem and merges index segments.
	Compact(ctx context.Context, ops domain.OpsScope) error
}

// BatchRows bounds one delete; the pause between batches lets ingest
// transactions interleave, so pruning cannot stall the writer.
const (
	BatchRows  = 5000
	batchPause = 10 * time.Millisecond
)

// Janitor prunes on demand (RunOnce) and remembers the last overdue figure.
type Janitor struct {
	store Store
	ret   domain.Retention
	now   func() time.Time
	pause func(context.Context, time.Duration) error

	mu      sync.Mutex
	overdue time.Duration
	lastRun time.Time
}

// New builds a janitor.
func New(store Store, ret domain.Retention, now func() time.Time) *Janitor {
	return &Janitor{store: store, ret: ret, now: now, pause: sleep}
}

// RunOnce prunes every (tenant, host) and kind, then measures ttl_overdue:
// the age of the oldest row past its retention, which should stay near zero.
// A host failing does not stop the others.
func (j *Janitor) RunOnce(ctx context.Context) (deleted int, err error) {
	ops, err := domain.NewOpsScope("retention janitor")
	if err != nil {
		return 0, err
	}
	targets, err := j.store.RetentionTargets(ctx, ops)
	if err != nil {
		return 0, fmt.Errorf("list retention targets: %w", err)
	}
	now := j.now()
	var errs []error
	var overdue time.Duration
	for _, t := range targets {
		for _, k := range domain.DataKinds {
			keep, ok := j.ret[k]
			if !ok {
				continue
			}
			cutoff := now.Add(-keep)
			n, err := j.prune(ctx, ops, t, k, cutoff)
			deleted += n
			if err != nil {
				errs = append(errs, fmt.Errorf("prune %s for host %v: %w", k, t.Host, err))
			}
			// Measured even when pruning failed: a stuck host is exactly
			// what ttl_overdue exists to show.
			oldest, any, err := j.store.OldestHost(ctx, ops, t, k)
			if err == nil && any && oldest.Before(cutoff) {
				overdue = max(overdue, cutoff.Sub(oldest))
			}
		}
	}
	if deleted > 0 {
		if err := j.store.Compact(ctx, ops); err != nil {
			errs = append(errs, fmt.Errorf("compact: %w", err))
		}
	}
	j.mu.Lock()
	j.overdue, j.lastRun = overdue, now
	j.mu.Unlock()
	return deleted, errors.Join(errs...)
}

func (j *Janitor) prune(ctx context.Context, ops domain.OpsScope, t Target, k domain.DataKind, cutoff time.Time) (int, error) {
	total := 0
	for {
		n, err := j.store.PruneHost(ctx, ops, t, k, cutoff, BatchRows)
		total += n
		if err != nil || n < BatchRows {
			return total, err
		}
		if err := j.pause(ctx, batchPause); err != nil {
			return total, err
		}
	}
}

// Overdue is ttl_overdue at the last run, and when that run was.
func (j *Janitor) Overdue() (time.Duration, time.Time) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.overdue, j.lastRun
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
