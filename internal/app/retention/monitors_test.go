package retention

import (
	"context"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// fakeMonitors holds old rows per (monitor, kind) and records cutoffs.
type fakeMonitors struct {
	targets []MonitorTarget
	old     map[int64]map[domain.DataKind]int
	oldest  time.Time
	cutoffs map[domain.DataKind]time.Time
}

func (f *fakeMonitors) MonitorRetentionTargets(context.Context, domain.OpsScope) ([]MonitorTarget, error) {
	return f.targets, nil
}
func (f *fakeMonitors) PruneMonitor(_ context.Context, _ domain.OpsScope, t MonitorTarget, k domain.DataKind, before time.Time, limit int) (int, error) {
	f.cutoffs[k] = before
	n := min(f.old[t.Monitor][k], limit)
	f.old[t.Monitor][k] -= n
	return n, nil
}
func (f *fakeMonitors) OldestMonitor(_ context.Context, _ domain.OpsScope, t MonitorTarget, k domain.DataKind) (time.Time, bool, error) {
	return f.oldest, f.old[t.Monitor][k] > 0, nil
}

func TestJanitorPrunesMonitorsUnderThePlan(t *testing.T) {
	day := 24 * time.Hour
	plan := domain.Plan{MaxHosts: 1, MetricsRetentionDays: 7, LogsRetentionDays: 1}
	m := &fakeMonitors{targets: []MonitorTarget{{Monitor: 1, Plan: plan}},
		old:     map[int64]map[domain.DataKind]int{1: {domain.DataMonitorResults: BatchRows + 3, domain.DataMonitorHourly: 2}},
		cutoffs: map[domain.DataKind]time.Time{}}
	hosts := &fakeStore{old: map[Target]map[domain.DataKind]int{}}
	j := New(hosts, domain.DefaultRetention, func() time.Time { return t0 })
	j.pause = func(context.Context, time.Duration) error { return nil }
	j.SetMonitorStore(m)
	n, err := j.RunOnce(context.Background())
	if err != nil || n != BatchRows+5 {
		t.Fatalf("deleted %d (%v), want every old monitor row in bounded batches", n, err)
	}
	if got := m.cutoffs[domain.DataMonitorResults]; !got.Equal(t0.Add(-7 * day)) {
		t.Fatalf("results cutoff %v: the plan's 7 days must cap the 30-day default", got)
	}
	if got := m.cutoffs[domain.DataMonitorIncidents]; !got.Equal(t0.Add(-7 * day)) {
		t.Fatalf("incidents cutoff %v, want the plan's 7 days", got)
	}
	if hosts.compacts != 1 {
		t.Fatalf("compacted %d times, want once after deleting monitor rows", hosts.compacts)
	}
}

func TestJanitorReportsOverdueMonitorData(t *testing.T) {
	m := &fakeMonitors{targets: []MonitorTarget{{Monitor: 1}}, oldest: t0.Add(-40 * 24 * time.Hour),
		old: map[int64]map[domain.DataKind]int{1: {domain.DataMonitorResults: 1}}, cutoffs: map[domain.DataKind]time.Time{}}
	stuck := &stuckMonitors{m}
	j := New(&fakeStore{old: map[Target]map[domain.DataKind]int{}}, domain.DefaultRetention, func() time.Time { return t0 })
	j.SetMonitorStore(stuck)
	_, _ = j.RunOnce(context.Background())
	if overdue, _ := j.Overdue(); overdue != 10*24*time.Hour {
		t.Fatalf("overdue %v: want 40-day-old results 10 days past a 30-day retention", overdue)
	}
}

// stuckMonitors never deletes anything, like a store whose deletes fail.
type stuckMonitors struct{ *fakeMonitors }

func (stuckMonitors) PruneMonitor(context.Context, domain.OpsScope, MonitorTarget, domain.DataKind, time.Time, int) (int, error) {
	return 0, nil
}
