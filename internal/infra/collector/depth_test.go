package collector

import (
	"context"
	"testing"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// TestDetailOnThisMachine: every detail sample the real collector produces
// must pass the server's catalog check, or it would be rejected at ingest.
func TestDetailOnThisMachine(t *testing.T) {
	d := NewDetail()
	_, _ = d.Collect(context.Background()) // prime per-core CPU times
	samples, errs := d.Collect(context.Background())
	for _, e := range errs {
		t.Logf("subsystem error (non-fatal): %v", e)
	}
	cores := 0
	for _, s := range samples {
		if err := domain.CheckDetail(s); err != nil {
			t.Fatalf("collected detail rejected by the catalog: %v (%+v)", err, s)
		}
		if s.Metric == domain.MetricCPUCore {
			cores++
		}
	}
	if cores == 0 {
		t.Fatal("no per-core CPU samples")
	}
}

func TestProcessesOnThisMachine(t *testing.T) {
	p := NewProcesses()
	if _, err := p.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	snap, err := p.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := snap.Validate(); err != nil {
		t.Fatalf("snapshot rejected by the domain: %v", err)
	}
	if len(snap.Procs) == 0 || len(snap.Procs) > 2*topN {
		t.Fatalf("%d processes, want 1..%d", len(snap.Procs), 2*topN)
	}
}

func TestTopIsUnionOfCPUAndRSSRankings(t *testing.T) {
	var all []domain.Process
	for i := 0; i < 50; i++ {
		all = append(all, domain.Process{Name: "p", CPUPercent: float64(i), RSS: uint64(50 - i)})
	}
	got := top(all)
	if len(got) != 2*topN {
		t.Fatalf("union of two disjoint top-%d lists has %d entries", topN, len(got))
	}
	both := []domain.Process{{Name: "x", CPUPercent: 9, RSS: 9}}
	if len(top(both)) != 1 {
		t.Fatal("a process in both rankings was listed twice")
	}
}
