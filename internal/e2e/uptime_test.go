package e2e

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/alerting"
	"github.com/MerhiOPS/sentrix/internal/app/uptime"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/netguard"
	"github.com/MerhiOPS/sentrix/internal/infra/probe"
	"github.com/MerhiOPS/sentrix/internal/infra/sqlite"
)

// recordingNotifier captures what would be sent to channels.
type recordingNotifier struct {
	mu   sync.Mutex
	sent []alerting.MonitorMessage
}

func (r *recordingNotifier) Notify(_ context.Context, _ domain.Channel, m alerting.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m.Monitor != nil {
		r.sent = append(r.sent, *m.Monitor)
	}
	return nil
}

func (r *recordingNotifier) events() []domain.MonitorEventKind {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.MonitorEventKind
	for _, m := range r.sent {
		out = append(out, m.Event)
	}
	return out
}

// runUptime starts the real checker and scheduler on st until the returned
// stop is called.
func runUptime(st *sqlite.Store, n alerting.Notifier) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	out := alerting.NewDispatcher(n, st, log)
	checker := uptime.NewChecker(uptime.CheckerDeps{Probe: probe.New(netguard.Policy{AllowPrivate: true}), States: st,
		Channels: st, Silences: st, Out: out})
	sched := uptime.NewScheduler(st, checker.Check, uptime.SchedulerConfig{Workers: 4, Reload: 200 * time.Millisecond}, time.Now, log)
	done := make(chan struct{}, 2)
	go func() { out.Run(ctx); done <- struct{}{} }()
	go func() { sched.Run(ctx); done <- struct{}{} }()
	return func() { cancel(); <-done; <-done }
}

func waitFor(t *testing.T, what string, within time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestUptimeIncidentSurvivesRestart: a real site goes down while the
// server runs, the server restarts mid-incident, the site recovers. One
// down and one up notification, the incident closed after the restart.
func TestUptimeIncidentSurvivesRestart(t *testing.T) {
	var failing atomic.Bool
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if failing.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer site.Close()
	ctx := context.Background()
	db, err := sqlite.OpenMigrated(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	st := sqlite.NewStore(db)
	ops, _ := domain.NewOpsScope("test")
	tid := domain.NewTenantID()
	if err := st.CreateTenant(ctx, ops, domain.Tenant{ID: tid, Name: "t", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	sc, _ := domain.NewScope(tid)
	ch, err := st.CreateChannel(ctx, sc, domain.Channel{Name: "ops", Kind: domain.ChannelWebhook, URL: "https://hooks.example/x"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.CreateMonitor(ctx, sc, domain.Monitor{Name: "site", Kind: domain.MonitorHTTP, Target: site.URL,
		Interval: time.Second, Timeout: 500 * time.Millisecond, ConfirmRetries: 1, Method: "GET", Channels: []int64{ch},
		CreatedAt: time.Now()}, 10)
	if err != nil {
		t.Fatal(err)
	}
	state := func() domain.MonitorState {
		states, err := st.MonitorStates(ctx, sc)
		if err != nil || len(states) != 1 {
			t.Fatalf("monitor states: %v, %v", states, err)
		}
		return states[0]
	}
	n := &recordingNotifier{}
	stop := runUptime(st, n)
	waitFor(t, "the monitor to be up", 5*time.Second, func() bool { return state().Status == domain.MonitorUp })
	failing.Store(true)
	waitFor(t, "a confirmed incident", 8*time.Second, func() bool { return state().Status == domain.MonitorDown })
	waitFor(t, "the down notification", 3*time.Second, func() bool { return len(n.events()) == 1 })
	stop() // the server goes away mid-incident

	failing.Store(false)
	n2 := &recordingNotifier{}
	stop = runUptime(st, n2)
	defer stop()
	waitFor(t, "recovery after the restart", 8*time.Second, func() bool { return state().Status == domain.MonitorUp })
	waitFor(t, "the up notification", 3*time.Second, func() bool { return len(n2.events()) == 1 })
	time.Sleep(1500 * time.Millisecond) // another check: nothing new to say
	if got := append(n.events(), n2.events()...); len(got) != 2 || got[0] != domain.EventDown || got[1] != domain.EventUp {
		t.Fatalf("notifications %v, want exactly [down up] across the restart", got)
	}
	inc, err := st.MonitorIncidents(ctx, sc, id, 10)
	if err != nil || len(inc) != 1 || inc[0].End == nil || inc[0].Failures < 2 || inc[0].Class != domain.ErrStatus {
		t.Fatalf("incidents %+v, want one, closed, with the failed checks counted", inc)
	}
	evs, err := st.ListMonitorEvents(ctx, sc, id, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if ev.Outcome != alerting.OutcomeDelivered {
			t.Fatalf("event %+v was not recorded as delivered", ev)
		}
	}
}
