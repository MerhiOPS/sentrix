package ingest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

var t0 = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// fakeStore records what CommitBatch was asked to write, and keeps counter
// state across calls the way a real store does.
type fakeStore struct {
	state    map[domain.HostID]CounterState
	seen     map[uint64]bool
	prepared []Prepared
	scopes   []domain.Scope
	err      error
}

func newFake() *fakeStore {
	return &fakeStore{state: map[domain.HostID]CounterState{}, seen: map[uint64]bool{}}
}

func (f *fakeStore) CommitBatch(_ context.Context, s domain.Scope, h domain.HostID, seq uint64, build BuildFunc) (CommitResult, error) {
	if f.err != nil {
		return CommitResult{}, f.err
	}
	f.scopes = append(f.scopes, s)
	if f.seen[seq] {
		return CommitResult{Replayed: true}, nil
	}
	p, err := build(f.state[h])
	if err != nil {
		return CommitResult{}, err
	}
	f.seen[seq] = true
	f.state[h] = p.Counters
	f.prepared = append(f.prepared, p)
	return CommitResult{Stored: len(p.Samples)}, nil
}

func sample(sec int, rx uint64) domain.CoreSample {
	return domain.CoreSample{TS: t0.Add(time.Duration(sec) * time.Second), NetRxBytes: rx}
}

func newSvc(st Store) *Service {
	return NewService(st, DefaultWindow, func() time.Time { return t0.Add(time.Minute) })
}

func authHost() AuthHost {
	return AuthHost{Tenant: domain.NewTenantID(), Host: domain.NewHostID(), Interval: 5 * time.Second, Enabled: true}
}

func TestIngestScopesByCredentialTenant(t *testing.T) {
	st := newFake()
	ah := authHost()
	if _, err := newSvc(st).Ingest(context.Background(), ah, Batch{Seq: 1, Samples: []domain.CoreSample{sample(0, 1)}}); err != nil {
		t.Fatal(err)
	}
	got, err := st.scopes[0].Tenant()
	if err != nil || got != ah.Tenant {
		t.Fatalf("scope tenant = %v (%v), want credential tenant %v", got, err, ah.Tenant)
	}
}

func TestIngestRejectsDisabledZeroSeqAndOversize(t *testing.T) {
	svc := newSvc(newFake())
	ah := authHost()
	disabled := ah
	disabled.Enabled = false
	if _, err := svc.Ingest(context.Background(), disabled, Batch{Seq: 1}); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("disabled host: err = %v, want ErrForbidden", err)
	}
	if _, err := svc.Ingest(context.Background(), ah, Batch{Seq: 0}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("seq 0: err = %v, want ErrInvalid", err)
	}
	big := Batch{Seq: 1, Samples: make([]domain.CoreSample, MaxSamples+1)}
	if _, err := svc.Ingest(context.Background(), ah, big); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("oversize: err = %v, want ErrInvalid", err)
	}
}

func TestIngestCounterResetNeverGoesBackwards(t *testing.T) {
	st := newFake()
	svc := newSvc(st)
	ah := authHost()
	// 1000 → 2000, reboot (raw 100), → 600; then a second reboot in the next batch.
	batches := [][]domain.CoreSample{
		{sample(0, 1000), sample(5, 2000), sample(10, 100), sample(15, 600)},
		{sample(20, 50), sample(25, 80)},
	}
	for i, b := range batches {
		if _, err := svc.Ingest(context.Background(), ah, Batch{Seq: uint64(i + 1), Samples: b}); err != nil {
			t.Fatal(err)
		}
	}
	var got []uint64
	for _, p := range st.prepared {
		for _, s := range p.Samples {
			got = append(got, s.NetRxBytes)
		}
	}
	want := []uint64{1000, 2000, 2100, 2600, 2650, 2680}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("adjusted counters %v, want %v", got, want)
		}
	}
}

func TestIngestClampsSnapsAndDedupes(t *testing.T) {
	st := newFake()
	svc := newSvc(st) // now = t0 + 1m
	ah := authHost()
	res, err := svc.Ingest(context.Background(), ah, Batch{Seq: 1, Samples: []domain.CoreSample{
		{TS: t0.Add(2 * time.Second)},                // snaps to t0
		{TS: t0.Add(1 * time.Second), CPUPercent: 9}, // same slot → dedupe, last wins
		{TS: t0.Add(7 * time.Second)},                // snaps to t0+5s
		{TS: t0.Add(365 * 24 * time.Hour)},           // a year ahead: rejected
		{TS: t0.Add(-73 * time.Hour)},                // older than MaxAge: rejected
		{TS: t0, CPUPercent: 101},                    // invalid: rejected
	}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Rejected != 3 {
		t.Errorf("rejected = %d, want 3", res.Rejected)
	}
	ss := st.prepared[0].Samples
	if len(ss) != 2 || !ss[0].TS.Equal(t0) || !ss[1].TS.Equal(t0.Add(5*time.Second)) {
		t.Fatalf("samples after normalize: %+v", ss)
	}
	if ss[0].CPUPercent != 9 {
		t.Errorf("dedupe kept the wrong sample: cpu=%v", ss[0].CPUPercent)
	}
}

func TestIngestReplayReportsNoRejections(t *testing.T) {
	st := newFake()
	svc := newSvc(st)
	ah := authHost()
	b := Batch{Seq: 9, Samples: []domain.CoreSample{sample(0, 1), {TS: t0.Add(1000 * time.Hour)}}}
	if _, err := svc.Ingest(context.Background(), ah, b); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Ingest(context.Background(), ah, b)
	if err != nil || !res.Replayed || res.Rejected != 0 || len(st.prepared) != 1 {
		t.Fatalf("replay: res=%+v err=%v prepared=%d", res, err, len(st.prepared))
	}
}

func TestIngestUsesHostIntervalWhenBatchIntervalInvalid(t *testing.T) {
	st := newFake()
	ah := authHost()
	ah.Interval = 10 * time.Second
	_, err := newSvc(st).Ingest(context.Background(), ah,
		Batch{Seq: 1, Interval: 1500 * time.Millisecond, Samples: []domain.CoreSample{{TS: t0.Add(6 * time.Second)}}})
	if err != nil {
		t.Fatal(err)
	}
	if p := st.prepared[0]; p.Interval != 10*time.Second || !p.Samples[0].TS.Equal(t0.Add(10*time.Second)) {
		t.Fatalf("interval=%v ts=%v", p.Interval, p.Samples[0].TS)
	}
}
