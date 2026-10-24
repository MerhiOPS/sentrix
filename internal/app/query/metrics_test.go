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
	host    domain.Host
	samples []domain.CoreSample
}

func (f fakeReader) GetHost(_ context.Context, _ domain.Scope, id domain.HostID) (domain.Host, error) {
	if id != f.host.ID {
		return domain.Host{}, domain.ErrNotFound
	}
	return f.host, nil
}
func (f fakeReader) ListHosts(context.Context, domain.Scope) ([]domain.Host, error) { return nil, nil }
func (f fakeReader) RawCore(_ context.Context, _ domain.Scope, _ domain.HostID, from, to time.Time) ([]domain.CoreSample, error) {
	var out []domain.CoreSample
	for _, s := range f.samples {
		if !s.TS.Before(from) && s.TS.Before(to) {
			out = append(out, s)
		}
	}
	return out, nil
}

func rig() (*Metrics, domain.Scope, domain.HostID) {
	h := domain.Host{ID: domain.NewHostID(), Interval: 5 * time.Second}
	f := fakeReader{host: h, samples: []domain.CoreSample{
		{TS: t0, CPUPercent: 10}, {TS: t0.Add(5 * time.Second), CPUPercent: 20},
		// gap at 10s
		{TS: t0.Add(15 * time.Second), CPUPercent: 40, MemoryPercent: 1},
	}}
	s, _ := domain.NewScope(domain.NewTenantID())
	return NewMetrics(f, f), s, h.ID
}

func TestSeriesNaturalResolutionWithGap(t *testing.T) {
	m, s, h := rig()
	f, err := m.Series(context.Background(), s, SeriesRequest{Host: h, Gauges: []Gauge{GaugeCPU}, From: t0, To: t0.Add(20 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	cpu := f.Series[GaugeCPU]
	if len(f.TS) != 4 || f.Step != 5*time.Second || cpu[0] != 10 || cpu[1] != 20 || !math.IsNaN(cpu[2]) || cpu[3] != 40 {
		t.Fatalf("frame = step %v ts %d cpu %v; the gap must be NaN, not 0", f.Step, len(f.TS), cpu)
	}
}

func TestSeriesBucketsAverage(t *testing.T) {
	m, s, h := rig()
	f, err := m.Series(context.Background(), s, SeriesRequest{Host: h, Gauges: []Gauge{GaugeCPU, GaugeMem},
		From: t0, To: t0.Add(20 * time.Second), Step: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Series[GaugeCPU]; len(got) != 2 || got[0] != 15 || got[1] != 40 {
		t.Fatalf("10 s buckets: %v, want [15 40]", got)
	}
}

func TestSeriesPointCapWithAndWithoutStep(t *testing.T) {
	m, s, h := rig()
	day := SeriesRequest{Host: h, Gauges: []Gauge{GaugeCPU}, From: t0, To: t0.Add(24 * time.Hour)}
	if _, err := m.Series(context.Background(), s, day); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("a day at 5 s (17280 points) without step: err = %v, want refused", err)
	}
	day.Step = time.Second
	if _, err := m.Series(context.Background(), s, day); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("a day at 1 s step: err = %v, want refused", err)
	}
	day.Step = time.Minute
	if f, err := m.Series(context.Background(), s, day); err != nil || len(f.TS) != 1440 {
		t.Fatalf("a day at 1 m: %d points, %v", len(f.TS), err)
	}
}

func TestSeriesValidation(t *testing.T) {
	m, s, h := rig()
	for name, req := range map[string]SeriesRequest{
		"to before from": {Host: h, Gauges: []Gauge{GaugeCPU}, From: t0, To: t0},
		"unknown gauge":  {Host: h, Gauges: []Gauge{"temp"}, From: t0, To: t0.Add(time.Minute)},
		"no gauges":      {Host: h, From: t0, To: t0.Add(time.Minute)},
		"sub-second":     {Host: h, Gauges: []Gauge{GaugeCPU}, From: t0, To: t0.Add(time.Minute), Step: time.Millisecond},
	} {
		if _, err := m.Series(context.Background(), s, req); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	if _, err := m.Series(context.Background(), s, SeriesRequest{Host: domain.NewHostID(), Gauges: []Gauge{GaugeCPU},
		From: t0, To: t0.Add(time.Minute)}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown host: err = %v", err)
	}
}
