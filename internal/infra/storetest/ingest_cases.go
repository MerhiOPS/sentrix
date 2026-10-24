package storetest

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

func commit(t *testing.T, st Stores, f fixture, seq uint64, b ingest.BuildFunc) ingest.CommitResult {
	t.Helper()
	r, err := st.CommitBatch(context.Background(), f.scope, f.host, seq, b)
	if err != nil {
		t.Fatalf("CommitBatch(seq %d): %v", seq, err)
	}
	return r
}

func readAll(t *testing.T, st Stores, f fixture) []domain.CoreSample {
	t.Helper()
	ss, err := st.RawCore(context.Background(), f.scope, f.host, T0.Add(-time.Hour), T0.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return ss
}

func testIngestStoresAndReads(t *testing.T, st Stores) {
	f := newFixture(t, st, "a")
	r := commit(t, st, f, 1, prepared(T0, []int{1, 0, 2}, []uint64{20, 10, 30}))
	if r.Stored != 3 || r.Conflicts != 0 || r.Replayed {
		t.Fatalf("result = %+v", r)
	}
	ss := readAll(t, st, f)
	if len(ss) != 3 || !ss[0].TS.Equal(T0) || ss[2].NetRxBytes != 30 {
		t.Fatalf("RawCore not ordered by time: %+v", ss)
	}
	// [from, to): a sample exactly at `to` is excluded.
	part, err := st.RawCore(context.Background(), f.scope, f.host, T0, T0.Add(10*time.Second))
	if err != nil || len(part) != 2 {
		t.Fatalf("half-open range returned %d samples, %v", len(part), err)
	}
}

func testReplayAppliesNothing(t *testing.T, st Stores) {
	f := newFixture(t, st, "a")
	commit(t, st, f, 7, prepared(T0, []int{0}, []uint64{1}))
	called := false
	r := commit(t, st, f, 7, func(s ingest.CounterState) (ingest.Prepared, error) {
		called = true
		return prepared(T0, []int{1, 2}, []uint64{2, 3})(s)
	})
	if !r.Replayed || called {
		t.Fatalf("replay: result %+v, build called=%v", r, called)
	}
	if n := len(readAll(t, st, f)); n != 1 {
		t.Fatalf("replay stored data: %d samples", n)
	}
}

func testConflictsCounted(t *testing.T, st Stores) {
	f := newFixture(t, st, "a")
	commit(t, st, f, 1, prepared(T0, []int{0, 1}, []uint64{1, 2}))
	r := commit(t, st, f, 2, prepared(T0, []int{1, 2}, []uint64{9, 3}))
	if r.Stored != 1 || r.Conflicts != 1 {
		t.Fatalf("result = %+v, want 1 stored 1 conflict", r)
	}
	if ss := readAll(t, st, f); ss[1].NetRxBytes != 2 {
		t.Fatalf("conflicting sample overwrote the stored one: %+v", ss[1])
	}
}

// testBuildErrorRollsBack: R1 — a failed build leaves no trace, so the agent's
// retry of the same seq is applied, not acked as a replay.
func testBuildErrorRollsBack(t *testing.T, st Stores) {
	f := newFixture(t, st, "a")
	_, err := st.CommitBatch(context.Background(), f.scope, f.host, 5, func(ingest.CounterState) (ingest.Prepared, error) {
		return ingest.Prepared{}, errors.New("boom")
	})
	if err == nil {
		t.Fatal("build error swallowed")
	}
	r := commit(t, st, f, 5, prepared(T0, []int{0}, []uint64{1}))
	if r.Replayed || r.Stored != 1 {
		t.Fatalf("retry after failed build: %+v", r)
	}
}

func testCounterStatePersists(t *testing.T, st Stores) {
	f := newFixture(t, st, "a")
	commit(t, st, f, 1, func(s ingest.CounterState) (ingest.Prepared, error) {
		p, err := prepared(T0, []int{0}, []uint64{1})(s)
		p.Counters = ingest.CounterState{ingest.NetRx: {LastRaw: 500, Offset: 1234}}
		return p, err
	})
	var seen ingest.CounterState
	commit(t, st, f, 2, func(s ingest.CounterState) (ingest.Prepared, error) {
		seen = s
		return prepared(T0, []int{1}, []uint64{2})(s)
	})
	if got := seen[ingest.NetRx]; got.LastRaw != 500 || got.Offset != 1234 {
		t.Fatalf("counter state not persisted: %+v", seen)
	}
}

func testSeqPruned(t *testing.T, st Stores) {
	f := newFixture(t, st, "a")
	commit(t, st, f, 1, prepared(T0, []int{0}, []uint64{1}))
	later := T0.Add(ingest.DefaultWindow.SeqRetention() + time.Minute)
	commit(t, st, f, 2, prepared(later, []int{1}, []uint64{2}))
	if r := commit(t, st, f, 1, prepared(later, []int{2}, []uint64{3})); r.Replayed {
		t.Fatal("seq older than retention was not pruned")
	}
	if r := commit(t, st, f, 2, prepared(later, nil, nil)); !r.Replayed {
		t.Fatal("seq within retention was pruned")
	}
}

func testLastSeenUpdated(t *testing.T, st Stores) {
	f := newFixture(t, st, "a")
	h, _ := st.GetHost(context.Background(), f.scope, f.host)
	if !h.LastSeen.IsZero() {
		t.Fatalf("fresh host has LastSeen %v", h.LastSeen)
	}
	at := T0.Add(3 * time.Minute)
	commit(t, st, f, 1, prepared(at, []int{0}, []uint64{1}))
	h, _ = st.GetHost(context.Background(), f.scope, f.host)
	if !h.LastSeen.Equal(at) || h.Status(at) != domain.StatusOK {
		t.Fatalf("LastSeen = %v, status %s", h.LastSeen, h.Status(at))
	}
}

// testHealthPersisted: what the agent reported and what the server refused
// are stored with the batch (Phase 1: "rejected and counted").
func testHealthPersisted(t *testing.T, st Stores) {
	f := newFixture(t, st, "a")
	report := func(rejected int, conflictsOf []int) ingest.BuildFunc {
		return func(s ingest.CounterState) (ingest.Prepared, error) {
			p, err := prepared(T0, conflictsOf, make([]uint64, len(conflictsOf)))(s)
			p.Rejected = rejected
			p.Agent = ingest.AgentStats{Version: "1.2.3", BufferDepth: 4, DroppedTotal: 5, ClockSkew: 1500 * time.Millisecond}
			return p, err
		}
	}
	commit(t, st, f, 1, report(2, []int{0}))
	commit(t, st, f, 2, report(3, []int{0, 1})) // ts 0 conflicts
	h, err := st.GetHost(context.Background(), f.scope, f.host)
	if err != nil {
		t.Fatal(err)
	}
	want := domain.HostHealth{BufferDepth: 4, DroppedTotal: 5, ClockSkew: 1500 * time.Millisecond, RejectedTotal: 5, ConflictTotal: 1}
	if h.Health != want || h.AgentVersion != "1.2.3" {
		t.Fatalf("health = %+v (version %q), want %+v", h.Health, h.AgentVersion, want)
	}
}

func testGaugePrecision(t *testing.T, st Stores) {
	f := newFixture(t, st, "a")
	commit(t, st, f, 1, prepared(T0, []int{0}, []uint64{1})) // cpu 12.345
	// The port contract is "round half away from zero to hundredths" of the
	// float64 value — the same expression both adapters must agree on.
	want := math.Round(12.345*100) / 100
	if got := readAll(t, st, f)[0].CPUPercent; got != want {
		t.Fatalf("cpu = %v, want %v (two-decimal precision)", got, want)
	}
}
