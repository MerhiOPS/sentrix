package storetest

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// coreAt builds a sample with distinct, checkable values.
func coreAt(sec int, cpu float64, rx uint64) domain.CoreSample {
	return domain.CoreSample{TS: T0.Add(time.Duration(sec) * time.Second), CPUPercent: cpu, MemoryPercent: cpu / 2,
		MemoryUsed: uint64(sec) * 1000, NetRxBytes: rx, DiskWriteBytes: rx * 2} //nolint:gosec // fixture: sec >= 0
}

func withSamples(at time.Time, ss ...domain.CoreSample) ingest.BuildFunc {
	return func(state ingest.CounterState) (ingest.Prepared, error) {
		return ingest.Prepared{Samples: ss, Counters: state, Interval: 5 * time.Second, ReceivedAt: at,
			SeqRetention: ingest.DefaultWindow.SeqRetention()}, nil
	}
}

// testRollupsEqualAggregationOfRaw: the contract that lets a ClickHouse
// adapter use materialized views instead — whatever maintains the rollups,
// they must equal an aggregation of the raw rows actually stored.
func testRollupsEqualAggregationOfRaw(t *testing.T, st Stores) {
	f := newFixture(t, st, "a")
	ctx := context.Background()
	// Samples straddle a minute boundary (55s → 65s) and the second batch
	// repeats ts=60s with a different value: a conflict, never aggregated.
	commit(t, st, f, 1, withSamples(T0, coreAt(50, 10, 100), coreAt(55, 20, 150), coreAt(60, 30, 200)))
	commit(t, st, f, 2, withSamples(T0, coreAt(60, 99, 999), coreAt(65, 40, 260), coreAt(120, 50, 400)))
	raw := readAll(t, st, f)
	for _, res := range []query.Resolution{query.Res1m, query.Res1h} {
		got, err := st.Rollups(ctx, f.scope, f.host, res, T0.Add(-time.Hour), T0.Add(2*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		want := aggregate(raw, res.Width())
		if len(got) != len(want) {
			t.Fatalf("%s: %d buckets, want %d", res, len(got), len(want))
		}
		for i := range want {
			compareBucket(t, string(res), got[i], want[i])
		}
	}
}

// aggregate is the test's own derivation of rollups from raw samples.
func aggregate(raw []domain.CoreSample, w time.Duration) []query.RollupBucket {
	var out []query.RollupBucket
	for _, s := range raw {
		start := s.TS.Truncate(w)
		if len(out) == 0 || !out[len(out)-1].Start.Equal(start) {
			out = append(out, query.RollupBucket{Start: start, FirstTS: s.TS,
				Gauges:   map[query.Gauge]query.Agg{query.GaugeCPU: {Min: math.Inf(1), Max: math.Inf(-1)}},
				Counters: map[string]query.Edge{query.CounterNetRx: {First: float64(s.NetRxBytes)}}})
		}
		b := &out[len(out)-1]
		b.N++
		a := b.Gauges[query.GaugeCPU]
		b.Gauges[query.GaugeCPU] = query.Agg{Sum: a.Sum + s.CPUPercent, Min: min(a.Min, s.CPUPercent), Max: max(a.Max, s.CPUPercent)}
		e := b.Counters[query.CounterNetRx]
		e.Last = float64(s.NetRxBytes)
		b.Counters[query.CounterNetRx] = e
		b.LastTS = s.TS
	}
	return out
}

func compareBucket(t *testing.T, res string, got, want query.RollupBucket) {
	t.Helper()
	g, w := got.Gauges[query.GaugeCPU], want.Gauges[query.GaugeCPU]
	c, wc := got.Counters[query.CounterNetRx], want.Counters[query.CounterNetRx]
	if !got.Start.Equal(want.Start) || got.N != want.N || math.Abs(g.Sum-w.Sum) > 1e-9 || g.Min != w.Min || g.Max != w.Max ||
		c != wc || !got.FirstTS.Equal(want.FirstTS) || !got.LastTS.Equal(want.LastTS) {
		t.Fatalf("%s bucket %v:\n got  n=%d cpu=%+v rx=%+v span=[%v,%v]\n want n=%d cpu=%+v rx=%+v span=[%v,%v]",
			res, want.Start, got.N, g, c, got.FirstTS, got.LastTS, want.N, w, wc, want.FirstTS, want.LastTS)
	}
}

func detail(sec int, metric string, kind domain.MetricKind, labels domain.Labels, v float64) domain.DetailSample {
	return domain.DetailSample{TS: T0.Add(time.Duration(sec) * time.Second), Metric: metric, Kind: kind, Labels: labels, Value: v}
}

func withDetails(seqRetention time.Duration, ds ...domain.DetailSample) ingest.BuildFunc {
	return func(state ingest.CounterState) (ingest.Prepared, error) {
		return ingest.Prepared{Details: ds, Counters: state, Interval: 5 * time.Second, ReceivedAt: T0, SeqRetention: seqRetention}, nil
	}
}

func testDetailSeriesAndPrecision(t *testing.T, st Stores) {
	f := newFixture(t, st, "a")
	ctx := context.Background()
	keep := ingest.DefaultWindow.SeqRetention()
	commit(t, st, f, 1, withDetails(keep,
		detail(0, domain.MetricCPUCore, domain.KindGauge, domain.Labels{"core": "0"}, 12.345),
		detail(5, domain.MetricCPUCore, domain.KindGauge, domain.Labels{"core": "0"}, 20),
		detail(0, domain.MetricNetRx, domain.KindCounter, domain.Labels{"nic": "eth0"}, 123456789)))
	series, err := st.ListSeries(ctx, f.scope, f.host)
	if err != nil || len(series) != 2 || series[0].Key.Metric != domain.MetricCPUCore || series[0].Labels["core"] != "0" {
		t.Fatalf("ListSeries = %+v, %v", series, err)
	}
	pts, err := st.DetailPoints(ctx, f.scope, f.host, series[0].ID, T0.Add(-time.Minute), T0.Add(time.Minute))
	if err != nil || len(pts) != 2 || pts[0].Value != math.Round(12.345*100)/100 {
		t.Fatalf("cpu core points = %+v, %v (percent at two decimals)", pts, err)
	}
	pts, _ = st.DetailPoints(ctx, f.scope, f.host, series[1].ID, T0.Add(-time.Minute), T0.Add(time.Minute))
	if len(pts) != 1 || pts[0].Value != 123456789 {
		t.Fatalf("byte counter points = %+v (bytes exact)", pts)
	}
}

func testSeriesCeilingRefusesOnlyNewSeries(t *testing.T, st Stores) {
	f := newFixture(t, st, "a")
	if err := st.SetSeriesLimit(context.Background(), mustOps(t), f.tenant, 1); err != nil {
		t.Fatal(err)
	}
	keep := ingest.DefaultWindow.SeqRetention()
	core := func(n string) domain.Labels { return domain.Labels{"core": n} }
	r := commit(t, st, f, 1, withDetails(keep,
		detail(0, domain.MetricCPUCore, domain.KindGauge, core("0"), 1),
		detail(0, domain.MetricCPUCore, domain.KindGauge, core("1"), 1)))
	if r.SeriesRefused != 1 {
		t.Fatalf("first batch: refused %d, want 1 (ceiling 1)", r.SeriesRefused)
	}
	r = commit(t, st, f, 2, withDetails(keep,
		detail(5, domain.MetricCPUCore, domain.KindGauge, core("0"), 2),  // existing: accepted
		detail(5, domain.MetricCPUCore, domain.KindGauge, core("2"), 2))) // new: refused
	if r.SeriesRefused != 1 {
		t.Fatalf("second batch: refused %d, want 1 — the existing series must not be blocked", r.SeriesRefused)
	}
	series, _ := st.ListSeries(context.Background(), f.scope, f.host)
	pts, _ := st.DetailPoints(context.Background(), f.scope, f.host, series[0].ID, T0.Add(-time.Minute), T0.Add(time.Minute))
	if len(series) != 1 || len(pts) != 2 {
		t.Fatalf("%d series, %d points; want 1 series with both points", len(series), len(pts))
	}
}

// testSeriesCreatedInRolledBackBatchIsNotCached: R1 for the series cache.
// A batch that creates a series and then fails must leave no usable trace.
func testSeriesCreatedInRolledBackBatchIsNotCached(t *testing.T, st Stores) {
	f := newFixture(t, st, "a")
	d := detail(0, domain.MetricNetTx, domain.KindCounter, domain.Labels{"nic": "eth9"}, 5)
	if _, err := st.CommitBatch(context.Background(), f.scope, f.host, 1, withDetails(0, d)); err == nil {
		t.Fatal("batch with zero seq retention must fail after writing its details")
	}
	commit(t, st, f, 1, withDetails(ingest.DefaultWindow.SeqRetention(), d))
	series, _ := st.ListSeries(context.Background(), f.scope, f.host)
	if len(series) != 1 {
		t.Fatalf("%d series after retry, want 1", len(series))
	}
	pts, err := st.DetailPoints(context.Background(), f.scope, f.host, series[0].ID, T0.Add(-time.Minute), T0.Add(time.Minute))
	if err != nil || len(pts) != 1 {
		t.Fatalf("retry after rollback: %d points, %v", len(pts), err)
	}
}

func testProcessesAtReturnsNewestNotAfter(t *testing.T, st Stores) {
	f := newFixture(t, st, "a")
	snap := func(sec int, name string) domain.ProcessSnapshot {
		return domain.ProcessSnapshot{TS: T0.Add(time.Duration(sec) * time.Second), Procs: []domain.Process{{Name: name, CPUPercent: 1.234, RSS: 42}}}
	}
	commit(t, st, f, 1, func(s ingest.CounterState) (ingest.Prepared, error) {
		return ingest.Prepared{Processes: []domain.ProcessSnapshot{snap(0, "a"), snap(30, "b")}, Counters: s,
			Interval: 5 * time.Second, ReceivedAt: T0, SeqRetention: ingest.DefaultWindow.SeqRetention()}, nil
	})
	ctx := context.Background()
	got, err := st.ProcessesAt(ctx, f.scope, f.host, T0.Add(45*time.Second))
	if err != nil || got.Procs[0].Name != "b" || got.Procs[0].CPUPercent != 1.23 || got.Procs[0].RSS != 42 {
		t.Fatalf("ProcessesAt(45s) = %+v, %v", got, err)
	}
	if got, _ := st.ProcessesAt(ctx, f.scope, f.host, T0.Add(29*time.Second)); got.Procs[0].Name != "a" {
		t.Fatalf("ProcessesAt(29s) returned a later snapshot: %+v", got)
	}
	if _, err := st.ProcessesAt(ctx, f.scope, f.host, T0.Add(-time.Second)); err == nil {
		t.Fatal("ProcessesAt before any snapshot must be ErrNotFound")
	}
}
