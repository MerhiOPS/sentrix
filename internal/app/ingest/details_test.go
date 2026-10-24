package ingest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

func rx(sec int, nic string, v float64) domain.DetailSample {
	return domain.DetailSample{TS: t0.Add(time.Duration(sec) * time.Second), Metric: domain.MetricNetRx,
		Kind: domain.KindCounter, Labels: domain.Labels{"nic": nic}, Value: v}
}

// TestLabelSpellingIsOneSeries: the canonical form ignores JSON spacing and
// key order, so differently formatted label maps dedupe as one series.
func TestLabelSpellingIsOneSeries(t *testing.T) {
	var a, b domain.Labels
	_ = json.Unmarshal([]byte(`{"core":"3"}`), &a)
	_ = json.Unmarshal([]byte(`{ "core" : "3" }`), &b)
	in := []domain.DetailSample{
		{TS: t0, Metric: domain.MetricCPUCore, Kind: domain.KindGauge, Labels: a, Value: 10},
		{TS: t0, Metric: domain.MetricCPUCore, Kind: domain.KindGauge, Labels: b, Value: 20},
	}
	out, rej := normalizeDetails(in, 5*time.Second, DefaultWindow, t0.Add(time.Minute))
	if rej != 0 || len(out) != 1 || out[0].Value != 20 {
		t.Fatalf("got %d samples (rejected %d): %+v — want one series, last wins", len(out), rej, out)
	}
}

func TestNormalizeDetailsRejectsUnknownAndOutOfWindow(t *testing.T) {
	in := []domain.DetailSample{
		rx(0, "eth0", 1),
		{TS: t0, Metric: "gpu_temp", Kind: domain.KindGauge, Labels: domain.Labels{"gpu": "0"}, Value: 50},
		rx(400*24*3600, "eth0", 1), // a year ahead
	}
	out, rej := normalizeDetails(in, 5*time.Second, DefaultWindow, t0.Add(time.Minute))
	if len(out) != 1 || rej != 2 {
		t.Fatalf("out %d rejected %d, want 1 and 2", len(out), rej)
	}
}

func TestDetailCountersFoldPerSeriesAcrossReboot(t *testing.T) {
	state := CounterState{}
	out, next := foldDetailCounters(state, []domain.DetailSample{
		rx(0, "eth0", 1000), rx(0, "eth1", 50), rx(5, "eth0", 2000), rx(10, "eth0", 100), rx(10, "eth1", 60),
	})
	got := []float64{out[0].Value, out[1].Value, out[2].Value, out[3].Value, out[4].Value}
	want := []float64{1000, 50, 2000, 2100, 60}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("folded %v, want %v (eth0 reboot folded, eth1 independent)", got, want)
		}
	}
	if len(state) != 0 {
		t.Fatal("input state was modified: folding must work on a draft")
	}
	if len(next) != 2 {
		t.Fatalf("state has %d counters, want one per series", len(next))
	}
}

func TestIngestFoldsCoreAndDetailCountersIndependently(t *testing.T) {
	st := newFake()
	svc := newSvc(st)
	ah := authHost()
	for i, b := range []Batch{
		{Seq: 1, Samples: []domain.CoreSample{sample(0, 500)}, Details: []domain.DetailSample{rx(0, "eth0", 9000)}},
		{Seq: 2, Samples: []domain.CoreSample{sample(5, 100)}, Details: []domain.DetailSample{rx(5, "eth0", 10000)}},
	} {
		if _, err := svc.Ingest(context.Background(), ah, b); err != nil {
			t.Fatalf("batch %d: %v", i, err)
		}
	}
	p := st.prepared[1]
	if p.Samples[0].NetRxBytes != 600 || p.Details[0].Value != 10000 {
		t.Fatalf("core %d (want 600, reset folded), detail %v (want 10000, no reset)", p.Samples[0].NetRxBytes, p.Details[0].Value)
	}
}

func TestSnapshotsValidatedAndDeduped(t *testing.T) {
	ok := domain.ProcessSnapshot{TS: t0, Procs: []domain.Process{{Name: "a", CPUPercent: 1}}}
	dup := domain.ProcessSnapshot{TS: t0.Add(300 * time.Millisecond), Procs: []domain.Process{{Name: "b"}}}
	bad := domain.ProcessSnapshot{Procs: []domain.Process{{Name: "c"}}} // no timestamp
	out, rej := normalizeSnapshots([]domain.ProcessSnapshot{ok, dup, bad}, DefaultWindow, t0.Add(time.Minute))
	if len(out) != 1 || rej != 1 || out[0].Procs[0].Name != "b" {
		t.Fatalf("out %+v rejected %d", out, rej)
	}
}
