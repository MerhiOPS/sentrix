package storetest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

func monitor(channels ...int64) domain.Monitor {
	return domain.Monitor{Name: "shop", Kind: domain.MonitorHTTP, Target: "https://shop.example.com/health",
		Interval: time.Minute, Timeout: 10 * time.Second, ConfirmRetries: 1, Method: "GET",
		AcceptStatus: []domain.StatusRange{{From: 200, To: 299}, {From: 301, To: 301}}, Keyword: "OK", KeywordMode: domain.KeywordPresent,
		KeywordCase: true, CertWarnDays: 14, Channels: channels, CreatedAt: T0}
}

func mustMonitor(t *testing.T, st Stores, s domain.Scope, m domain.Monitor) int64 {
	t.Helper()
	id, err := st.CreateMonitor(context.Background(), s, m, 100)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func stateOf(t *testing.T, st Stores, s domain.Scope, id int64) domain.MonitorState {
	t.Helper()
	for _, x := range must(st.MonitorStates(context.Background(), s)) {
		if x.Monitor == id {
			return x
		}
	}
	t.Fatalf("no state for monitor %d", id)
	return domain.MonitorState{}
}

func testMonitorRoundTrip(t *testing.T, st Stores) {
	ctx := context.Background()
	f := newFixture(t, st, "a")
	c1, c2 := mustChannel(t, st, f.scope, "one"), mustChannel(t, st, f.scope, "two")
	want := monitor(c2, c1)
	id := mustMonitor(t, st, f.scope, want)
	want.ID, want.Channels = id, []int64{c1, c2}
	got, err := st.GetMonitor(ctx, f.scope, id)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("GetMonitor = %+v, %v; want %+v", got, err, want)
	}
	if s := stateOf(t, st, f.scope, id); s.Status != domain.MonitorPending || !s.Since.Equal(T0) || !s.LastCheck.IsZero() {
		t.Fatalf("a new monitor's state is %+v, want pending since creation", s)
	}
	tcp := domain.Monitor{ID: id, Name: "db", Kind: domain.MonitorTCP, Target: "db.example.com:5432", Interval: 2 * time.Minute,
		Timeout: 5 * time.Second, Channels: []int64{c2}, CreatedAt: T0}
	if err := st.UpdateMonitor(ctx, f.scope, tcp, T0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if list := must(st.ListMonitors(ctx, f.scope)); len(list) != 1 || !reflect.DeepEqual(list[0], tcp) {
		t.Fatalf("after update ListMonitors = %+v; want %+v", list, tcp)
	}
	if err := st.DeleteChannel(ctx, f.scope, c2); err != nil {
		t.Fatal(err)
	}
	if got := must(st.GetMonitor(ctx, f.scope, id)); len(got.Channels) != 0 {
		t.Fatalf("a deleted channel is still linked: %v", got.Channels)
	}
	if err := st.DeleteMonitor(ctx, f.scope, id); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetMonitor(ctx, f.scope, id); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted monitor: %v", err)
	}
	for name, err := range map[string]error{
		"update": st.UpdateMonitor(ctx, f.scope, tcp, T0),
		"delete": st.DeleteMonitor(ctx, f.scope, id),
		"pause":  st.SetPaused(ctx, f.scope, id, true, T0),
	} {
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s of a deleted monitor: %v, want ErrNotFound", name, err)
		}
	}
}

func testMonitorQuotaAndRefs(t *testing.T, st Stores) {
	ctx := context.Background()
	f := newFixture(t, st, "a")
	if _, err := st.CreateMonitor(ctx, f.scope, monitor(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateMonitor(ctx, f.scope, monitor(), 1); !errors.Is(err, domain.ErrQuotaExceeded) {
		t.Fatalf("second monitor on a plan of one: %v, want ErrQuotaExceeded", err)
	}
	if _, err := st.CreateMonitor(ctx, f.scope, monitor(), 0); !errors.Is(err, domain.ErrQuotaExceeded) {
		t.Fatalf("a plan of zero monitors: %v", err)
	}
	if _, err := st.CreateMonitor(ctx, f.scope, monitor(424242), 10); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown channel: %v, want ErrNotFound", err)
	}
	if n := len(must(st.ListMonitors(ctx, f.scope))); n != 1 {
		t.Fatalf("%d monitors stored, want only the first", n)
	}
	if u := must(st.GetUsage(ctx, f.scope, T0)); u.Monitors != 1 {
		t.Fatalf("usage counts %d monitors, want 1", u.Monitors)
	}
}

func testMonitorPauseResumeAndRetarget(t *testing.T, st Stores) {
	ctx := context.Background()
	f := newFixture(t, st, "a")
	id := mustMonitor(t, st, f.scope, monitor())
	m := must(st.GetMonitor(ctx, f.scope, id))
	recordCycleToDown(t, st, f.scope, m)
	if len(openIncidents(t, st, f.scope, id)) != 1 {
		t.Fatal("setup: the monitor should be down with an open incident")
	}
	pauseAt := T0.Add(time.Hour)
	if err := st.SetPaused(ctx, f.scope, id, true, pauseAt); err != nil {
		t.Fatal(err)
	}
	s := stateOf(t, st, f.scope, id)
	if s.Status != domain.MonitorPaused || !s.Since.Equal(pauseAt) || s.Failures != 0 || !must(st.GetMonitor(ctx, f.scope, id)).Paused {
		t.Fatalf("paused state %+v", s)
	}
	if inc := must(st.MonitorIncidents(ctx, f.scope, id, 10)); len(inc) != 1 || inc[0].End == nil || !inc[0].End.Equal(pauseAt) {
		t.Fatalf("pausing must end the open incident at the pause: %+v", inc)
	}
	rev := s.Revision
	if err := st.SetPaused(ctx, f.scope, id, true, pauseAt.Add(time.Minute)); err != nil || stateOf(t, st, f.scope, id).Revision != rev {
		t.Fatalf("pausing a paused monitor must be a no-op: %v", err)
	}
	if err := st.SetPaused(ctx, f.scope, id, false, pauseAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if s := stateOf(t, st, f.scope, id); s.Status != domain.MonitorPending || s.Revision == rev {
		t.Fatalf("resumed state %+v, want pending with a new revision", s)
	}
	// An edit that keeps the target keeps the state; a new target starts over.
	recordCycleToDown(t, st, f.scope, m)
	m.Name = "renamed"
	if err := st.UpdateMonitor(ctx, f.scope, m, T0.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if s := stateOf(t, st, f.scope, id); s.Status != domain.MonitorDown {
		t.Fatalf("a rename reset the state: %+v", s)
	}
	m.Target = "https://other.example.com/"
	if err := st.UpdateMonitor(ctx, f.scope, m, T0.Add(4*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if s := stateOf(t, st, f.scope, id); s.Status != domain.MonitorPending || len(openIncidents(t, st, f.scope, id)) != 0 {
		t.Fatalf("a new target must reset the state and end the incident: %+v", s)
	}
}

func openIncidents(t *testing.T, st Stores, s domain.Scope, id int64) []domain.Incident {
	t.Helper()
	var out []domain.Incident
	for _, in := range must(st.MonitorIncidents(context.Background(), s, id, 100)) {
		if in.End == nil {
			out = append(out, in)
		}
	}
	return out
}

func testScheduledMonitorsAcrossTenants(t *testing.T, st Stores) {
	ctx := context.Background()
	a, b := newFixture(t, st, "a"), newFixture(t, st, "b")
	ch := mustChannel(t, st, a.scope, "ops")
	ma := mustMonitor(t, st, a.scope, monitor(ch))
	mb := mustMonitor(t, st, b.scope, monitor())
	paused := mustMonitor(t, st, b.scope, monitor())
	if err := st.SetPaused(ctx, b.scope, paused, true, T0); err != nil {
		t.Fatal(err)
	}
	got, err := st.ScheduledMonitors(ctx, mustOps(t))
	if err != nil || len(got) != 2 {
		t.Fatalf("ScheduledMonitors = %+v, %v; want the two unpaused monitors", got, err)
	}
	for _, s := range got {
		want := map[int64]domain.TenantID{ma: a.tenant, mb: b.tenant}[s.Monitor.ID]
		if s.Tenant != want || s.State.Monitor != s.Monitor.ID || s.State.Status != domain.MonitorPending {
			t.Fatalf("scheduled %+v", s)
		}
		if s.Monitor.ID == ma && !reflect.DeepEqual(s.Monitor.Channels, []int64{ch}) {
			t.Fatalf("scheduled monitor lost its channels: %v", s.Monitor.Channels)
		}
	}
	if _, err := st.ScheduledMonitors(ctx, domain.OpsScope{}); err == nil {
		t.Fatal("ScheduledMonitors ran without an OpsScope")
	}
}
