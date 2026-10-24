package e2e

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/agent"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Server modes for outage tests.
const (
	modeUp      = iota
	modeDown    // connection dropped before anything is read: a killed server
	modeLoseAck // the batch is stored, then the connection drops before the ack
)

// serve applies the stack's mode. Dropping the connection (not a 5xx) is
// what a killed process looks like to the agent.
func (s *stack) serve(h http.Handler, w http.ResponseWriter, r *http.Request) {
	switch s.mode.Load() {
	case modeDown:
		hangUp(w)
	case modeLoseAck:
		h.ServeHTTP(httptest.NewRecorder(), r) // stored; the agent never hears
		hangUp(w)
	default:
		h.ServeHTTP(w, r)
	}
}

func hangUp(w http.ResponseWriter) {
	if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
		_ = conn.Close()
	}
}

// gridCollector stamps samples on an exact 1 s grid, so every collected
// sample has its own slot: stored rows must equal samples kept.
type gridCollector struct {
	base   time.Time
	n      atomic.Int64
	starts int // agent runs: each discards its first reading (CPU priming)
}

// kept is how many samples the agent kept: every reading but the priming one
// of each start.
func (g *gridCollector) kept() int { return int(g.n.Load()) - g.starts }

func (g *gridCollector) Collect(context.Context) (domain.CoreSample, []error) {
	n := g.n.Add(1)
	return domain.CoreSample{TS: g.base.Add(time.Duration(n) * time.Second), CPUPercent: 10, NetRxBytes: uint64(n) * 100}, nil
}

func (s *stack) runAgent(t *testing.T, stateDir string, id agent.Identity, col agent.Collector, d time.Duration) {
	t.Helper()
	cfg := agent.DefaultConfig(stateDir)
	cfg.Interval, cfg.BatchEvery = time.Second, time.Second
	if g, ok := col.(*gridCollector); ok {
		g.starts++
	}
	a, err := agent.New(cfg, id, agent.Sources{Core: col}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	a.Run(ctx)
}

func (s *stack) stored(t *testing.T, id agent.Identity) int {
	t.Helper()
	hid, _ := domain.ParseHostID(id.HostID)
	ss, err := s.store.RawCore(context.Background(), s.scope, hid, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return len(ss)
}

// TestServerOutageLosesNothing: Phase 6 acceptance, in-process. The server
// vanishes mid-run; the agent keeps sampling into the WAL, reconnects with
// backoff and replays: rows stored equal samples collected, none twice.
// The two-minute version against the real binaries is scripts/outage.sh.
func TestServerOutageLosesNothing(t *testing.T) {
	s := newStack(t)
	stateDir := t.TempDir()
	id, err := agent.Enroll(context.Background(), stateDir, s.srv.URL, s.token(t, s.pin), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	col := &gridCollector{base: time.Now().Add(-time.Minute)}
	go func() {
		time.Sleep(2 * time.Second)
		s.mode.Store(modeDown)
		time.Sleep(4 * time.Second)
		s.mode.Store(modeUp)
	}()
	s.runAgent(t, stateDir, id, col, 14*time.Second)
	if got, want := s.stored(t, id), col.kept(); got != want {
		t.Fatalf("stored %d rows for %d samples kept across the outage", got, want)
	}
}

// TestAgentKilledThenRestartedNoDuplicates: the agent dies with a WAL full of
// unsent batches and one batch whose ack was lost after the server stored it.
// The restarted agent resends all of them; the server's replay gate keeps
// each sample exactly once.
func TestAgentKilledThenRestartedNoDuplicates(t *testing.T) {
	s := newStack(t)
	stateDir := t.TempDir()
	id, err := agent.Enroll(context.Background(), stateDir, s.srv.URL, s.token(t, s.pin), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	col := &gridCollector{base: time.Now().Add(-time.Minute)}
	s.mode.Store(modeLoseAck)
	s.runAgent(t, stateDir, id, col, 3*time.Second) // stored, never acknowledged: all still in the WAL
	if s.stored(t, id) == 0 {
		t.Fatal("lose-ack mode stored nothing; the test would prove nothing")
	}
	s.mode.Store(modeDown)
	s.runAgent(t, stateDir, id, col, 3*time.Second) // nothing reaches the server: more WAL
	s.mode.Store(modeUp)
	s.runAgent(t, stateDir, id, col, 6*time.Second) // a fresh process on the same state dir
	if got, want := s.stored(t, id), col.kept(); got != want {
		t.Fatalf("stored %d rows for %d samples: lost or duplicated across agent restarts", got, want)
	}
	hid, _ := domain.ParseHostID(id.HostID)
	if h, _ := s.store.GetHost(context.Background(), s.scope, hid); h.Health.ConflictTotal != 0 {
		t.Fatalf("%d samples hit an existing row: resent batches were re-applied, not recognised as replays", h.Health.ConflictTotal)
	}
}
