package query

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// MaxPoints is the most points one series request may return. A request
// over it is refused, never silently downsampled into something the caller
// did not ask for (OLD B1/B8) — the caller passes a coarser step.
const MaxPoints = 2000

// Gauge names a core gauge that can be charted.
type Gauge string

const (
	GaugeCPU     Gauge = "cpu"
	GaugeMem     Gauge = "mem"
	GaugeDisk    Gauge = "disk"
	GaugeLoad1   Gauge = "load1"
	GaugeLoad5   Gauge = "load5"
	GaugeLoad15  Gauge = "load15"
	GaugeMemUsed Gauge = "mem_used"
)

var gaugeValue = map[Gauge]func(domain.CoreSample) float64{
	GaugeCPU:     func(s domain.CoreSample) float64 { return s.CPUPercent },
	GaugeMem:     func(s domain.CoreSample) float64 { return s.MemoryPercent },
	GaugeDisk:    func(s domain.CoreSample) float64 { return s.DiskPercent },
	GaugeLoad1:   func(s domain.CoreSample) float64 { return s.Load1 },
	GaugeLoad5:   func(s domain.CoreSample) float64 { return s.Load5 },
	GaugeLoad15:  func(s domain.CoreSample) float64 { return s.Load15 },
	GaugeMemUsed: func(s domain.CoreSample) float64 { return float64(s.MemoryUsed) },
}

// SeriesRequest asks for gauges of one host over [From, To).
type SeriesRequest struct {
	Host   domain.HostID
	Gauges []Gauge
	From   time.Time
	To     time.Time
	Step   time.Duration // zero = the host's own sampling interval
}

// Frame is columnar: one timestamp axis, one value column per gauge. A NaN
// value is a bucket with no data; the transport renders it as null, so a
// chart draws a gap instead of a fake zero (OLD B13).
type Frame struct {
	From, To time.Time
	Step     time.Duration
	TS       []time.Time
	Series   map[Gauge][]float64
}

// Metrics answers chart queries.
type Metrics struct {
	hosts HostReader
	core  CoreReader
}

// NewMetrics builds the use case.
func NewMetrics(hosts HostReader, core CoreReader) *Metrics {
	return &Metrics{hosts: hosts, core: core}
}

// Series returns one Frame. Each bucket holds the mean of the samples in it.
func (m *Metrics) Series(ctx context.Context, s domain.Scope, req SeriesRequest) (Frame, error) {
	if err := validate(req); err != nil {
		return Frame{}, err
	}
	h, err := m.hosts.GetHost(ctx, s, req.Host)
	if err != nil {
		return Frame{}, err
	}
	step := req.Step
	if step == 0 {
		step = max(h.Interval, domain.MinInterval)
	}
	n := int(math.Ceil(float64(req.To.Sub(req.From)) / float64(step)))
	if n > MaxPoints {
		return Frame{}, fmt.Errorf("%w: %d points requested, max %d — use a coarser step", domain.ErrInvalid, n, MaxPoints)
	}
	samples, err := m.core.RawCore(ctx, s, req.Host, req.From, req.To)
	if err != nil {
		return Frame{}, err
	}
	return bucket(req, step, n, samples), nil
}

func validate(req SeriesRequest) error {
	if !req.To.After(req.From) {
		return fmt.Errorf("%w: to must be after from", domain.ErrInvalid)
	}
	if req.Step < 0 || (req.Step > 0 && req.Step < domain.MinInterval) {
		return fmt.Errorf("%w: step must be at least %v", domain.ErrInvalid, domain.MinInterval)
	}
	if len(req.Gauges) == 0 {
		return fmt.Errorf("%w: no gauges requested", domain.ErrInvalid)
	}
	for _, g := range req.Gauges {
		if _, ok := gaugeValue[g]; !ok {
			return fmt.Errorf("%w: unknown gauge %q", domain.ErrInvalid, g)
		}
	}
	return nil
}

// bucket averages samples into n buckets of width step starting at From.
func bucket(req SeriesRequest, step time.Duration, n int, samples []domain.CoreSample) Frame {
	f := Frame{From: req.From, To: req.To, Step: step, TS: make([]time.Time, n), Series: map[Gauge][]float64{}}
	sums := map[Gauge][]float64{}
	counts := make([]int, n)
	for _, g := range req.Gauges {
		sums[g] = make([]float64, n)
	}
	for i := range f.TS {
		f.TS[i] = req.From.Add(time.Duration(i) * step)
	}
	for _, smp := range samples {
		i := int(smp.TS.Sub(req.From) / step)
		if i < 0 || i >= n {
			continue
		}
		counts[i]++
		for _, g := range req.Gauges {
			sums[g][i] += gaugeValue[g](smp)
		}
	}
	for _, g := range req.Gauges {
		col := make([]float64, n)
		for i := range col {
			if counts[i] == 0 {
				col[i] = math.NaN()
			} else {
				col[i] = sums[g][i] / float64(counts[i])
			}
		}
		f.Series[g] = col
	}
	return f
}
