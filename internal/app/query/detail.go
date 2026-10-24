package query

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// DetailRequest asks for one detail series over [From, To).
type DetailRequest struct {
	Host   domain.HostID
	Series int64
	From   time.Time
	To     time.Time
	Step   time.Duration // zero = automatic
}

// DetailFrame is one detail series bucketed: gauge means, or per-second
// rates for counters. NaN marks a bucket without data.
type DetailFrame struct {
	From, To time.Time
	Step     time.Duration
	Kind     domain.MetricKind
	TS       []time.Time
	Values   []float64
}

// Details answers per-core / per-mount / per-device / per-NIC charts.
type Details struct {
	hosts  HostReader
	detail DetailReader
}

// NewDetails builds the use case.
func NewDetails(hosts HostReader, detail DetailReader) *Details {
	return &Details{hosts: hosts, detail: detail}
}

// Series lists a host's detail series.
func (d *Details) Series(ctx context.Context, s domain.Scope, host domain.HostID) ([]domain.Series, error) {
	return d.detail.ListSeries(ctx, s, host)
}

// Frame buckets one series. Detail rows are short-retention raw data, so
// there is no rollup source; the same point cap applies.
func (d *Details) Frame(ctx context.Context, s domain.Scope, req DetailRequest) (DetailFrame, error) {
	if !req.To.After(req.From) || req.Step < 0 || (req.Step > 0 && req.Step < domain.MinInterval) {
		return DetailFrame{}, fmt.Errorf("%w: bad range or step", domain.ErrInvalid)
	}
	h, err := d.hosts.GetHost(ctx, s, req.Host)
	if err != nil {
		return DetailFrame{}, err
	}
	all, err := d.detail.ListSeries(ctx, s, req.Host)
	if err != nil {
		return DetailFrame{}, err
	}
	kind, ok := kindOf(all, req.Series)
	if !ok {
		return DetailFrame{}, fmt.Errorf("series: %w", domain.ErrNotFound)
	}
	step, _, err := plan(SeriesRequest{From: req.From, To: req.To, Step: req.Step}, max(h.Interval, domain.MinInterval))
	if err != nil {
		return DetailFrame{}, err
	}
	pts, err := d.detail.DetailPoints(ctx, s, req.Host, req.Series, req.From, req.To)
	if err != nil {
		return DetailFrame{}, err
	}
	return bucketDetail(req, step, kind, pts), nil
}

func kindOf(all []domain.Series, id int64) (domain.MetricKind, bool) {
	for _, s := range all {
		if s.ID == id {
			return s.Kind, true
		}
	}
	return "", false
}

func bucketDetail(req DetailRequest, step time.Duration, kind domain.MetricKind, pts []DetailPoint) DetailFrame {
	n := int(math.Ceil(float64(req.To.Sub(req.From)) / float64(step)))
	f := DetailFrame{From: req.From, To: req.To, Step: step, Kind: kind, TS: make([]time.Time, n), Values: make([]float64, n)}
	type b struct {
		sum, count, first, last float64
		firstTS, lastTS         time.Time
	}
	bs := make([]*b, n)
	for i := range f.TS {
		f.TS[i] = req.From.Add(time.Duration(i) * step)
		f.Values[i] = math.NaN()
	}
	for _, p := range pts {
		i := int(p.TS.Sub(req.From) / step)
		if i < 0 || i >= n {
			continue
		}
		if bs[i] == nil {
			bs[i] = &b{first: p.Value, firstTS: p.TS}
		}
		bs[i].sum += p.Value
		bs[i].count++
		bs[i].last, bs[i].lastTS = p.Value, p.TS
	}
	for i, x := range bs {
		switch {
		case x == nil:
		case kind == domain.KindCounter:
			if span := x.lastTS.Sub(x.firstTS).Seconds(); span > 0 {
				f.Values[i] = (x.last - x.first) / span // covered span (OLD B5)
			}
		default:
			f.Values[i] = x.sum / x.count
		}
	}
	return f
}
