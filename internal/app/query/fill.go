package query

import (
	"math"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

func newFrame(req SeriesRequest, step time.Duration, src Source) Frame {
	n := int(math.Ceil(float64(req.To.Sub(req.From)) / float64(step)))
	f := Frame{From: req.From, To: req.To, Step: step, Source: src, TS: make([]time.Time, n), Series: map[Gauge][]float64{}}
	for i := range f.TS {
		f.TS[i] = req.From.Add(time.Duration(i) * step)
	}
	for _, g := range req.Gauges {
		col := make([]float64, n)
		for i := range col {
			col[i] = math.NaN()
		}
		f.Series[g] = col
	}
	return f
}

// acc accumulates one output bucket from either source.
type acc struct {
	n        float64
	sum      map[Gauge]float64
	first    map[string]float64
	last     map[string]float64
	firstTS  time.Time
	lastTS   time.Time
	hasEdges bool
}

func newAcc() *acc {
	return &acc{sum: map[Gauge]float64{}, first: map[string]float64{}, last: map[string]float64{}}
}

// observe records a span's first and last counter values, keeping the
// earliest first and the latest last. Both edges are taken in one call: a
// bucket's first edge must never move lastTS without its last values.
func (a *acc) observe(firstTS time.Time, first map[string]float64, lastTS time.Time, last map[string]float64) {
	if !a.hasEdges || firstTS.Before(a.firstTS) {
		a.firstTS = firstTS
		for c, v := range first {
			a.first[c] = v
		}
	}
	if !a.hasEdges || lastTS.After(a.lastTS) {
		a.lastTS = lastTS
		for c, v := range last {
			a.last[c] = v
		}
	}
	a.hasEdges = true
}

// emit writes the bucket into column i. Gauges are means. A rate divides
// the counter's growth by the span its samples cover — never the bucket
// width (OLD B5): a bucket with an interior gap is not diluted. Stored
// counters are reset-adjusted, so growth is never negative; one sample
// (zero span) is no rate at all.
func (a *acc) emit(f *Frame, i int, gauges []Gauge) {
	span := a.lastTS.Sub(a.firstTS).Seconds()
	for _, g := range gauges {
		if c, isRate := rateCounter[g]; isRate {
			if span > 0 {
				f.Series[g][i] = (a.last[c] - a.first[c]) / span
			}
			continue
		}
		if a.n > 0 {
			f.Series[g][i] = a.sum[g] / a.n
		}
	}
}

func counterValues(s domain.CoreSample) map[string]float64 {
	return map[string]float64{
		CounterNetRx: float64(s.NetRxBytes), CounterNetTx: float64(s.NetTxBytes),
		CounterDiskR: float64(s.DiskReadBytes), CounterDiskW: float64(s.DiskWriteBytes),
	}
}

func fillFromRaw(f *Frame, gauges []Gauge, samples []domain.CoreSample) {
	accs := map[int]*acc{}
	for _, s := range samples {
		i := int(s.TS.Sub(f.From) / f.Step)
		if i < 0 || i >= len(f.TS) {
			continue
		}
		a, ok := accs[i]
		if !ok {
			a = newAcc()
			accs[i] = a
		}
		a.n++
		for g, val := range gaugeValue {
			a.sum[g] += val(s)
		}
		cv := counterValues(s)
		a.observe(s.TS, cv, s.TS, cv)
	}
	for i, a := range accs {
		a.emit(f, i, gauges)
	}
}

func fillFromRollups(f *Frame, gauges []Gauge, buckets []RollupBucket) {
	accs := map[int]*acc{}
	for _, b := range buckets {
		if b.Start.Before(f.From) && b.LastTS.Before(f.From) {
			continue
		}
		i := int(max(b.Start.Sub(f.From), 0) / f.Step)
		if i >= len(f.TS) {
			continue
		}
		a, ok := accs[i]
		if !ok {
			a = newAcc()
			accs[i] = a
		}
		a.n += float64(b.N)
		for g, agg := range b.Gauges {
			a.sum[g] += agg.Sum
		}
		first, last := map[string]float64{}, map[string]float64{}
		for c, e := range b.Counters {
			first[c], last[c] = e.First, e.Last
		}
		a.observe(b.FirstTS, first, b.LastTS, last)
	}
	for i, a := range accs {
		a.emit(f, i, gauges)
	}
}
