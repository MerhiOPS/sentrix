package query

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

var t0 = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

type fakeReader struct {
	host        domain.Host
	samples     []domain.CoreSample
	buckets     map[Resolution][]RollupBucket
	rawReads    int
	rollupReads map[Resolution]int
}

func (f *fakeReader) GetHost(_ context.Context, _ domain.Scope, id domain.HostID) (domain.Host, error) {
	if id != f.host.ID {
		return domain.Host{}, domain.ErrNotFound
	}
	return f.host, nil
}
func (f *fakeReader) ListHosts(context.Context, domain.Scope) ([]domain.Host, error) { return nil, nil }
func (f *fakeReader) RawCore(_ context.Context, _ domain.Scope, _ domain.HostID, from, to time.Time) ([]domain.CoreSample, error) {
	f.rawReads++
	var out []domain.CoreSample
	for _, s := range f.samples {
		if !s.TS.Before(from) && s.TS.Before(to) {
			out = append(out, s)
		}
	}
	return out, nil
}
func (f *fakeReader) Rollups(_ context.Context, _ domain.Scope, _ domain.HostID, res Resolution, from, to time.Time) ([]RollupBucket, error) {
	f.rollupReads[res]++
	var out []RollupBucket
	for _, b := range f.buckets[res] {
		if !b.Start.Before(from) && b.Start.Before(to) {
			out = append(out, b)
		}
	}
	return out, nil
}

func rig() (*Metrics, *fakeReader, domain.Scope) {
	f := &fakeReader{host: domain.Host{ID: domain.NewHostID(), Interval: 5 * time.Second},
		buckets: map[Resolution][]RollupBucket{}, rollupReads: map[Resolution]int{}}
	s, _ := domain.NewScope(domain.NewTenantID())
	return NewMetrics(f, f, f), f, s
}

func at(sec int, cpu float64, rx uint64) domain.CoreSample {
	return domain.CoreSample{TS: t0.Add(time.Duration(sec) * time.Second), CPUPercent: cpu, NetRxBytes: rx}
}

func req(f *fakeReader, from, to time.Time, step time.Duration, g ...Gauge) SeriesRequest {
	return SeriesRequest{Host: f.host.ID, Gauges: g, From: from, To: to, Step: step}
}

func TestSeriesRawWithNullGap(t *testing.T) {
	m, f, s := rig()
	f.samples = []domain.CoreSample{at(0, 10, 0), at(5, 20, 0), at(15, 40, 0)}
	fr, err := m.Series(context.Background(), s, req(f, t0, t0.Add(20*time.Second), 5*time.Second, GaugeCPU))
	if err != nil {
		t.Fatal(err)
	}
	cpu := fr.Series[GaugeCPU]
	if fr.Source != SourceRaw || cpu[0] != 10 || cpu[1] != 20 || !math.IsNaN(cpu[2]) || cpu[3] != 40 {
		t.Fatalf("source %s cpu %v; the gap must be NaN, not 0", fr.Source, cpu)
	}
}

func TestSourceFollowsWindow(t *testing.T) {
	m, f, s := rig()
	for _, c := range []struct {
		window time.Duration
		want   Source
	}{{time.Hour, SourceRaw}, {6 * time.Hour, Source1m}, {7 * 24 * time.Hour, Source1m}, {30 * 24 * time.Hour, Source1h}} {
		fr, err := m.Series(context.Background(), s, req(f, t0.Add(-c.window), t0, 0, GaugeCPU))
		if err != nil {
			t.Fatal(err)
		}
		if fr.Source != c.want || len(fr.TS) > TargetPoints+1 {
			t.Errorf("window %v: source %s with %d points, want %s and <= %d", c.window, fr.Source, len(fr.TS), c.want, TargetPoints+1)
		}
	}
	if f.rollupReads[Res1h] != 1 {
		t.Fatalf("the 30-day chart did not read the 1h rollup (reads: %v, raw %d)", f.rollupReads, f.rawReads)
	}
}

// TestRateDividesByCoveredSpan: OLD B5. Samples at 0 s and 5 s, a gap, then
// 55 s — the rate is growth / 55 s, not growth / the 60 s bucket.
func TestRateDividesByCoveredSpan(t *testing.T) {
	m, f, s := rig()
	f.samples = []domain.CoreSample{at(0, 0, 0), at(5, 0, 500), at(55, 0, 5500)}
	fr, err := m.Series(context.Background(), s, req(f, t0, t0.Add(time.Minute), time.Minute, RateNetRx))
	if err != nil {
		t.Fatal(err)
	}
	if got := fr.Series[RateNetRx][0]; math.Abs(got-100) > 1e-9 {
		t.Fatalf("rate = %v B/s, want 100 (5500 B over the 55 s covered)", got)
	}
}

func TestRateFromRollupsUsesEdgesAcrossBuckets(t *testing.T) {
	m, f, s := rig()
	f.buckets[Res1m] = []RollupBucket{
		{Start: t0, N: 12, FirstTS: t0, LastTS: t0.Add(55 * time.Second), Counters: map[string]Edge{CounterNetRx: {First: 0, Last: 5500}}},
		{Start: t0.Add(time.Minute), N: 1, FirstTS: t0.Add(65 * time.Second), LastTS: t0.Add(65 * time.Second),
			Counters: map[string]Edge{CounterNetRx: {First: 6500, Last: 6500}}},
	}
	fr, err := m.Series(context.Background(), s, req(f, t0, t0.Add(2*time.Minute), 2*time.Minute, RateNetRx))
	if err != nil {
		t.Fatal(err)
	}
	if fr.Source != Source1m || math.Abs(fr.Series[RateNetRx][0]-100) > 1e-9 {
		t.Fatalf("source %s rate %v, want 1m and 100 B/s (6500 over 65 s)", fr.Source, fr.Series[RateNetRx])
	}
}

// TestCounterResetNeverGivesNegativeRate: stored counters are reset-adjusted
// (folded at ingest), so a reboot shows as continued growth.
func TestCounterResetNeverGivesNegativeRate(t *testing.T) {
	m, f, s := rig()
	// raw 1000, 2000, reboot → 100, 600: stored adjusted as 1000, 2000, 2100, 2600.
	f.samples = []domain.CoreSample{at(0, 0, 1000), at(5, 0, 2000), at(10, 0, 2100), at(15, 0, 2600)}
	fr, err := m.Series(context.Background(), s, req(f, t0, t0.Add(20*time.Second), 10*time.Second, RateNetRx))
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range fr.Series[RateNetRx] {
		if v < 0 {
			t.Fatalf("bucket %d: negative rate %v", i, v)
		}
	}
}

func TestSeriesPointCapWithAndWithoutStep(t *testing.T) {
	m, f, s := rig()
	day := req(f, t0, t0.Add(24*time.Hour), time.Second, GaugeCPU)
	if _, err := m.Series(context.Background(), s, day); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("a day at 1 s step: err = %v, want refused", err)
	}
	day.Step = 0 // automatic: never over the cap
	if fr, err := m.Series(context.Background(), s, day); err != nil || len(fr.TS) > MaxPoints {
		t.Fatalf("automatic step: %d points, %v", len(fr.TS), err)
	}
}

func TestSeriesValidation(t *testing.T) {
	m, f, s := rig()
	for name, r := range map[string]SeriesRequest{
		"to before from": req(f, t0, t0, 0, GaugeCPU),
		"unknown metric": req(f, t0, t0.Add(time.Minute), 0, "temp"),
		"no metrics":     req(f, t0, t0.Add(time.Minute), 0),
		"sub-second":     req(f, t0, t0.Add(time.Minute), time.Millisecond, GaugeCPU),
	} {
		if _, err := m.Series(context.Background(), s, r); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	r := req(f, t0, t0.Add(time.Minute), 0, GaugeCPU)
	r.Host = domain.NewHostID()
	if _, err := m.Series(context.Background(), s, r); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown host: err = %v", err)
	}
}
