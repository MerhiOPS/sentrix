package retention

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

var t0 = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// fakeStore holds, per target and kind, how many old rows remain and the
// oldest timestamp; failing targets error on prune.
type fakeStore struct {
	targets  []Target
	old      map[Target]map[domain.DataKind]int
	oldest   map[Target]time.Time
	fail     map[Target]bool
	calls    int
	compacts int
}

func (f *fakeStore) RetentionTargets(context.Context, domain.OpsScope) ([]Target, error) {
	return f.targets, nil
}
func (f *fakeStore) PruneHost(_ context.Context, _ domain.OpsScope, t Target, k domain.DataKind, _ time.Time, limit int) (int, error) {
	f.calls++
	if f.fail[t] {
		return 0, errors.New("disk I/O error")
	}
	n := min(f.old[t][k], limit)
	f.old[t][k] -= n
	return n, nil
}
func (f *fakeStore) OldestHost(_ context.Context, _ domain.OpsScope, t Target, k domain.DataKind) (time.Time, bool, error) {
	if f.old[t][k] > 0 {
		return f.oldest[t], true, nil
	}
	return time.Time{}, false, nil
}
func (f *fakeStore) Compact(context.Context, domain.OpsScope) error { f.compacts++; return nil }

func TestJanitorDrainsInBatchesAndReportsOverdue(t *testing.T) {
	ok, broken := Target{Host: domain.NewHostID()}, Target{Host: domain.NewHostID()}
	f := &fakeStore{targets: []Target{broken, ok},
		old:    map[Target]map[domain.DataKind]int{ok: {domain.DataRawCore: 2*BatchRows + 7}, broken: {domain.DataLogs: 3}},
		oldest: map[Target]time.Time{broken: t0.Add(-30 * 24 * time.Hour)}, fail: map[Target]bool{broken: true}}
	j := New(f, domain.Retention{domain.DataRawCore: 7 * 24 * time.Hour, domain.DataLogs: 14 * 24 * time.Hour}, func() time.Time { return t0 })
	j.pause = func(context.Context, time.Duration) error { return nil }
	n, err := j.RunOnce(context.Background())
	if n != 2*BatchRows+7 || f.old[ok][domain.DataRawCore] != 0 {
		t.Fatalf("deleted %d, left %d: want every old row in bounded batches", n, f.old[ok][domain.DataRawCore])
	}
	if err == nil {
		t.Fatal("a failing host was not reported")
	}
	overdue, at := j.Overdue()
	if overdue != 16*24*time.Hour || !at.Equal(t0) {
		t.Fatalf("overdue %v: want the stuck host's 30-day-old logs, 16 days past a 14-day retention", overdue)
	}
	if f.compacts != 1 {
		t.Fatalf("compacted %d times, want once after deleting", f.compacts)
	}
}

func TestJanitorSkipsKindsWithoutRetention(t *testing.T) {
	tg := Target{Host: domain.NewHostID()}
	f := &fakeStore{targets: []Target{tg}, old: map[Target]map[domain.DataKind]int{tg: {domain.DataLogs: 5}}}
	j := New(f, domain.Retention{domain.DataRawCore: time.Hour}, func() time.Time { return t0 })
	if n, err := j.RunOnce(context.Background()); n != 0 || err != nil || f.calls != 1 || f.compacts != 0 {
		t.Fatalf("deleted %d (%v) with %d prune calls: logs have no retention configured here", n, err, f.calls)
	}
}
