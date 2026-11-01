package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/uptime"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// record runs results through domain.StepMonitor and RecordCheck, as the
// scheduler would, and returns the event ids of the last one.
func record(t *testing.T, st Stores, s domain.Scope, m domain.Monitor, results ...domain.CheckResult) []int64 {
	t.Helper()
	var ids []int64
	for _, r := range results {
		prev := stateOf(t, st, s, m.ID)
		step := domain.StepMonitor(m, prev, r)
		for i := range step.Events {
			step.Events[i].Notify = true
		}
		var err error
		ids, err = st.RecordCheck(context.Background(), s, uptime.CheckRecord{Monitor: m, Result: r, Step: step, Revision: prev.Revision})
		if err != nil {
			t.Fatalf("RecordCheck at %s: %v", r.At, err)
		}
	}
	return ids
}

func result(at time.Duration, o domain.CheckOutcome, latency time.Duration) domain.CheckResult {
	r := domain.CheckResult{At: T0.Add(at), Outcome: o, Latency: latency, StatusCode: 200}
	if o == domain.CheckDown {
		r.Class, r.StatusCode = domain.ErrStatus, 503
	}
	return r
}

// recordCycleToDown takes a monitor from up to a confirmed incident.
func recordCycleToDown(t *testing.T, st Stores, s domain.Scope, m domain.Monitor) {
	t.Helper()
	last := stateOf(t, st, s, m.ID).LastCheck
	base := max(last.Sub(T0)+time.Minute, 0)
	record(t, st, s, m, result(base, domain.CheckUp, 100*time.Millisecond),
		result(base+time.Minute, domain.CheckDown, 0), result(base+time.Minute+10*time.Second, domain.CheckDown, 0))
}

func testMonitorCheckCycle(t *testing.T, st Stores) {
	ctx := context.Background()
	f := newFixture(t, st, "a")
	m := must(st.GetMonitor(ctx, f.scope, mustMonitor(t, st, f.scope, monitor())))
	m.Interval = 30 * time.Minute // so the 58-minute gap below is vouched for
	// 12:00 up; 12:58 up; 12:59 first failure; 12:59:10 confirmed; 13:01 still down;
	// 13:02 recovered. Downtime 12:59 → 13:02 spans the hour boundary.
	record(t, st, f.scope, m,
		result(0, domain.CheckUp, 120*time.Millisecond),
		result(59*time.Minute-time.Minute, domain.CheckUp, 80*time.Millisecond), // 12:58
		result(59*time.Minute, domain.CheckDown, 0),
		result(59*time.Minute+10*time.Second, domain.CheckDown, 0),
		result(61*time.Minute, domain.CheckDown, 0))
	if s := stateOf(t, st, f.scope, m.ID); s.Status != domain.MonitorDown || !s.Since.Equal(T0.Add(59*time.Minute)) {
		t.Fatalf("state %+v, want down since the first failure", s)
	}
	inc := openIncidents(t, st, f.scope, m.ID)
	if len(inc) != 1 || inc[0].Failures != 3 || inc[0].Class != domain.ErrStatus || !inc[0].Start.Equal(T0.Add(59*time.Minute)) {
		t.Fatalf("open incident %+v, want one since 12:59 with 3 failed checks", inc)
	}
	ids := record(t, st, f.scope, m, result(62*time.Minute, domain.CheckUp, 90*time.Millisecond))
	if len(ids) != 1 || len(openIncidents(t, st, f.scope, m.ID)) != 0 {
		t.Fatalf("recovery: event ids %v, open incidents %v", ids, openIncidents(t, st, f.scope, m.ID))
	}
	evs := must(st.ListMonitorEvents(ctx, f.scope, m.ID, 10))
	if len(evs) != 2 || evs[0].ID != ids[0] || evs[0].Kind != domain.EventUp || evs[0].DownFor != 3*time.Minute ||
		evs[1].Kind != domain.EventDown || evs[1].Class != domain.ErrStatus || !evs[0].Notify {
		t.Fatalf("events %+v, want [up after 3m, down], newest first", evs)
	}
	hours := must(st.MonitorHours(ctx, f.scope, m.ID, T0, T0.Add(2*time.Hour)))
	if len(hours) != 2 {
		t.Fatalf("hours %+v, want 12:00 and 13:00", hours)
	}
	h12, h13 := hours[0], hours[1]
	if h12.Up != 59*time.Minute || h12.Down != time.Minute || h12.Checks != 4 || h12.Failures != 2 {
		t.Fatalf("12:00 %+v, want 59m up, 1m down, 4 checks, 2 failures", h12)
	}
	if h13.Up != 0 || h13.Down != 2*time.Minute || h13.Checks != 2 || h13.Failures != 1 {
		t.Fatalf("13:00 %+v, want 2m down, 2 checks, 1 failure", h13)
	}
	if h12.LatencyN != 2 || h12.LatencySum != 200*time.Millisecond || h12.LatencyMax != 120*time.Millisecond {
		t.Fatalf("12:00 latency %+v: failed checks measure no latency", h12)
	}
	res := must(st.MonitorResults(ctx, f.scope, m.ID, T0, T0.Add(2*time.Hour), 3))
	if len(res) != 3 || !res[2].At.Equal(T0.Add(62*time.Minute)) || res[0].StatusCode != 503 || res[0].Class != domain.ErrStatus {
		t.Fatalf("results %+v, want the newest 3, oldest first", res)
	}
	if u := must(st.GetUsage(ctx, f.scope, T0)); u.MonthChecks != 6 {
		t.Fatalf("metered %d checks, want 6", u.MonthChecks)
	}
	if all := must(st.ListMonitorEvents(ctx, f.scope, 0, 1)); len(all) != 1 || all[0].ID != ids[0] {
		t.Fatalf("ListMonitorEvents(all, 1) = %+v", all)
	}
	// A second incident opens and closes without touching the first.
	record(t, st, f.scope, m, result(70*time.Minute, domain.CheckDown, 0), result(70*time.Minute+10*time.Second, domain.CheckDown, 0),
		result(80*time.Minute, domain.CheckUp, time.Millisecond))
	inc = must(st.MonitorIncidents(ctx, f.scope, m.ID, 10))
	if len(inc) != 2 || inc[1].End == nil || !inc[1].End.Equal(T0.Add(62*time.Minute)) ||
		inc[0].End == nil || !inc[0].End.Equal(T0.Add(80*time.Minute)) {
		t.Fatalf("incidents %+v: want the first ended at 13:02 and the second at 13:20, newest first", inc)
	}
}

func testMonitorStaleCheckRefused(t *testing.T, st Stores) {
	ctx := context.Background()
	f := newFixture(t, st, "a")
	m := must(st.GetMonitor(ctx, f.scope, mustMonitor(t, st, f.scope, monitor())))
	prev := stateOf(t, st, f.scope, m.ID)
	r := result(0, domain.CheckDown, 0)
	stale := uptime.CheckRecord{Monitor: m, Result: r, Step: domain.StepMonitor(m, prev, r), Revision: prev.Revision}
	if err := st.SetPaused(ctx, f.scope, m.ID, true, T0); err != nil {
		t.Fatal(err)
	}
	for name, rec := range map[string]uptime.CheckRecord{
		"paused while checked": stale,
		"old revision":         {Monitor: m, Result: r, Step: stale.Step, Revision: stateOf(t, st, f.scope, m.ID).Revision},
	} {
		if _, err := st.RecordCheck(ctx, f.scope, rec); !errors.Is(err, uptime.ErrStale) {
			t.Errorf("%s: %v, want ErrStale", name, err)
		}
	}
	if err := st.SetPaused(ctx, f.scope, m.ID, false, T0); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RecordCheck(ctx, f.scope, stale); !errors.Is(err, uptime.ErrStale) {
		t.Fatalf("a check started before a pause and resume: %v, want ErrStale", err)
	}
	if err := st.DeleteMonitor(ctx, f.scope, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RecordCheck(ctx, f.scope, stale); !errors.Is(err, uptime.ErrStale) {
		t.Fatalf("a check of a deleted monitor: %v, want ErrStale", err)
	}
	if u := must(st.GetUsage(ctx, f.scope, T0)); u.MonthChecks != 0 {
		t.Fatalf("a refused check was metered: %d", u.MonthChecks)
	}
}

func testMonitorSweepUndelivered(t *testing.T, st Stores) {
	ctx := context.Background()
	f := newFixture(t, st, "a")
	m := must(st.GetMonitor(ctx, f.scope, mustMonitor(t, st, f.scope, monitor())))
	recordCycleToDown(t, st, f.scope, m)
	ids := record(t, st, f.scope, m, result(10*time.Minute, domain.CheckUp, time.Millisecond))
	if err := st.SetMonitorDelivery(ctx, f.scope, ids[0], "delivered", ""); err != nil {
		t.Fatal(err)
	}
	n, err := st.FailUndeliveredMonitorEvents(ctx, mustOps(t), "restarted")
	if err != nil || n != 1 {
		t.Fatalf("swept %d (%v), want the one undelivered down event", n, err)
	}
	evs := must(st.ListMonitorEvents(ctx, f.scope, m.ID, 10))
	if evs[0].Outcome != "delivered" || evs[1].Outcome != "failed" || evs[1].Delivery != "restarted" {
		t.Fatalf("events after sweep %+v", evs)
	}
	if err := st.SetMonitorDelivery(ctx, f.scope, ids[0]+999, "delivered", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown event: %v", err)
	}
}

func testMonitorRetention(t *testing.T, st Stores) {
	ctx := context.Background()
	f := newFixture(t, st, "a")
	m := must(st.GetMonitor(ctx, f.scope, mustMonitor(t, st, f.scope, monitor())))
	recordCycleToDown(t, st, f.scope, m)                                             // checks at 12:00, 12:01, 12:01:10: an open incident
	record(t, st, f.scope, m, result(3*time.Hour, domain.CheckUp, time.Millisecond)) // closed at 15:00
	recordCycleToDown(t, st, f.scope, m)                                             // a second, open incident
	ops := mustOps(t)
	targets := must(st.MonitorRetentionTargets(ctx, ops))
	if len(targets) != 1 || targets[0].Monitor != m.ID || targets[0].Tenant != f.tenant || targets[0].Plan.MaxMonitors != domain.DefaultPlan.MaxMonitors {
		t.Fatalf("targets %+v", targets)
	}
	tg := targets[0]
	cut := T0.Add(2 * time.Hour)
	if n := must(st.PruneMonitor(ctx, ops, tg, domain.DataMonitorResults, cut, 2)); n != 2 {
		t.Fatalf("pruned %d results, want the limit of 2", n)
	}
	if oldest, ok, _ := st.OldestMonitor(ctx, ops, tg, domain.DataMonitorResults); !ok || !oldest.Equal(T0.Add(70*time.Second)) {
		t.Fatalf("oldest result %v %v: pruning must go oldest first", oldest, ok)
	}
	must(st.PruneMonitor(ctx, ops, tg, domain.DataMonitorResults, cut, 100))
	if res := must(st.MonitorResults(ctx, f.scope, m.ID, T0, T0.Add(24*time.Hour), 100)); len(res) == 0 || res[0].At.Before(cut) {
		t.Fatalf("results before the cutoff survived: %+v", res)
	}
	must(st.PruneMonitor(ctx, ops, tg, domain.DataMonitorHourly, cut, 100))
	if hrs := must(st.MonitorHours(ctx, f.scope, m.ID, T0, T0.Add(24*time.Hour))); len(hrs) == 0 || hrs[0].Hour.Before(cut) {
		t.Fatalf("hours before the cutoff survived: %+v", hrs)
	}
	must(st.PruneMonitor(ctx, ops, tg, domain.DataMonitorIncidents, T0.Add(24*time.Hour), 100))
	if inc := must(st.MonitorIncidents(ctx, f.scope, m.ID, 10)); len(inc) != 1 || inc[0].End != nil {
		t.Fatalf("incidents %+v: the ended one is pruned, the open one never", inc)
	}
	if _, err := st.PruneMonitor(ctx, ops, tg, domain.DataLogs, cut, 1); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("a host data kind: %v, want ErrInvalid", err)
	}
}
