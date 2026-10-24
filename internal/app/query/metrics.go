package query

import (
	"context"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// MaxPoints is the most points one series request may return. A request
// over it is refused, never silently downsampled into something the caller
// did not ask for (OLD B1/B8) — the caller passes a coarser step.
const MaxPoints = 2000

// Gauge names a chartable core metric: a gauge, or a counter's rate.
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

// SeriesRequest asks for core metrics of one host over [From, To).
type SeriesRequest struct {
	Host   domain.HostID
	Gauges []Gauge
	From   time.Time
	To     time.Time
	Step   time.Duration // zero = automatic (TargetPoints)
}

// Frame is columnar: one timestamp axis, one value column per metric. NaN
// is a bucket without data; the transport renders it as null, so a chart
// draws a gap instead of a fake zero (OLD B13).
type Frame struct {
	From, To time.Time
	Step     time.Duration
	Source   Source
	TS       []time.Time
	Series   map[Gauge][]float64
}

// Metrics answers chart queries.
type Metrics struct {
	hosts   HostReader
	core    CoreReader
	rollups RollupReader
}

// NewMetrics builds the use case.
func NewMetrics(hosts HostReader, core CoreReader, rollups RollupReader) *Metrics {
	return &Metrics{hosts: hosts, core: core, rollups: rollups}
}

// Series returns one Frame from raw samples or rollups, whichever plan picks.
func (m *Metrics) Series(ctx context.Context, s domain.Scope, req SeriesRequest) (Frame, error) {
	if err := validate(req); err != nil {
		return Frame{}, err
	}
	h, err := m.hosts.GetHost(ctx, s, req.Host)
	if err != nil {
		return Frame{}, err
	}
	step, source, err := plan(req, max(h.Interval, domain.MinInterval))
	if err != nil {
		return Frame{}, err
	}
	f := newFrame(req, step, source)
	if source == SourceRaw {
		samples, err := m.core.RawCore(ctx, s, req.Host, req.From, req.To)
		if err != nil {
			return Frame{}, err
		}
		fillFromRaw(&f, req.Gauges, samples)
		return f, nil
	}
	res := Res1m
	if source == Source1h {
		res = Res1h
	}
	// Buckets are keyed by start; widen the read so a bucket starting before
	// From that ends inside it is still found, then filter in fillFromRollups.
	buckets, err := m.rollups.Rollups(ctx, s, req.Host, res, req.From.Truncate(res.Width()), req.To)
	if err != nil {
		return Frame{}, err
	}
	fillFromRollups(&f, req.Gauges, buckets)
	return f, nil
}

func validate(req SeriesRequest) error {
	if !req.To.After(req.From) {
		return fmt.Errorf("%w: to must be after from", domain.ErrInvalid)
	}
	if req.Step < 0 || (req.Step > 0 && req.Step < domain.MinInterval) {
		return fmt.Errorf("%w: step must be at least %v", domain.ErrInvalid, domain.MinInterval)
	}
	if len(req.Gauges) == 0 {
		return fmt.Errorf("%w: no metrics requested", domain.ErrInvalid)
	}
	for _, g := range req.Gauges {
		if !isKnown(g) {
			return fmt.Errorf("%w: unknown metric %q", domain.ErrInvalid, g)
		}
	}
	return nil
}
