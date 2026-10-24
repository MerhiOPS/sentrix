package memstore

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Rollups implements query.RollupReader by aggregating raw samples at read
// time — deliberately a different method from SQLite's incremental upserts,
// so the contract compares two independent derivations.
func (s *Store) Rollups(_ context.Context, sc domain.Scope, id domain.HostID, res query.Resolution, from, to time.Time) ([]query.RollupBucket, error) {
	if res != query.Res1m && res != query.Res1h {
		return nil, fmt.Errorf("%w: resolution %q", domain.ErrInvalid, res)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	h, err := s.hostIn(sc, id)
	if err != nil {
		return nil, err
	}
	w := res.Width()
	buckets := map[int64]*query.RollupBucket{}
	for _, smp := range h.samples {
		start := smp.TS.Truncate(w)
		if start.Before(from) || !start.Before(to) {
			continue
		}
		b, ok := buckets[start.UnixMilli()]
		if !ok {
			b = &query.RollupBucket{Start: start, Gauges: map[query.Gauge]query.Agg{}, Counters: map[string]query.Edge{}}
			buckets[start.UnixMilli()] = b
		}
		addToBucket(b, roundGauges(smp))
	}
	out := make([]query.RollupBucket, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

func addToBucket(b *query.RollupBucket, s domain.CoreSample) {
	gauges := map[query.Gauge]float64{
		query.GaugeCPU: s.CPUPercent, query.GaugeMem: s.MemoryPercent, query.GaugeDisk: s.DiskPercent,
		query.GaugeLoad1: s.Load1, query.GaugeLoad5: s.Load5, query.GaugeLoad15: s.Load15,
		query.GaugeMemUsed: float64(s.MemoryUsed),
	}
	counters := map[string]float64{
		query.CounterNetRx: float64(s.NetRxBytes), query.CounterNetTx: float64(s.NetTxBytes),
		query.CounterDiskR: float64(s.DiskReadBytes), query.CounterDiskW: float64(s.DiskWriteBytes),
	}
	first := b.N == 0
	b.N++
	for g, v := range gauges {
		a := b.Gauges[g]
		if first {
			a = query.Agg{Sum: v, Min: v, Max: v}
		} else {
			a = query.Agg{Sum: a.Sum + v, Min: min(a.Min, v), Max: max(a.Max, v)}
		}
		b.Gauges[g] = a
	}
	for c, v := range counters {
		e := b.Counters[c]
		if first || s.TS.Before(b.FirstTS) {
			e.First = v
		}
		if first || s.TS.After(b.LastTS) {
			e.Last = v
		}
		b.Counters[c] = e
	}
	if first || s.TS.Before(b.FirstTS) {
		b.FirstTS = s.TS
	}
	if first || s.TS.After(b.LastTS) {
		b.LastTS = s.TS
	}
}

// ListSeries implements query.DetailReader.
func (s *Store) ListSeries(_ context.Context, sc domain.Scope, id domain.HostID) ([]domain.Series, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, err := s.hostIn(sc, id)
	if err != nil {
		return nil, err
	}
	var out []domain.Series
	for _, ms := range h.series {
		out = append(out, ms.s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key.Metric != out[j].Key.Metric {
			return out[i].Key.Metric < out[j].Key.Metric
		}
		return out[i].Labels.Canonical() < out[j].Labels.Canonical()
	})
	return out, nil
}

// DetailPoints implements query.DetailReader.
func (s *Store) DetailPoints(_ context.Context, sc domain.Scope, id domain.HostID, series int64, from, to time.Time) ([]query.DetailPoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, err := s.hostIn(sc, id)
	if err != nil {
		return nil, err
	}
	for _, ms := range h.series {
		if ms.s.ID != series {
			continue
		}
		var out []query.DetailPoint
		for ts, v := range ms.points {
			t := time.UnixMilli(ts).UTC()
			if !t.Before(from) && t.Before(to) {
				out = append(out, query.DetailPoint{TS: t, Value: v})
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].TS.Before(out[j].TS) })
		return out, nil
	}
	return nil, fmt.Errorf("series: %w", domain.ErrNotFound)
}

// ProcessesAt implements query.ProcessReader.
func (s *Store) ProcessesAt(_ context.Context, sc domain.Scope, id domain.HostID, at time.Time) (domain.ProcessSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, err := s.hostIn(sc, id)
	if err != nil {
		return domain.ProcessSnapshot{}, err
	}
	var best *domain.ProcessSnapshot
	for _, sn := range h.snaps {
		if !sn.TS.After(at) && (best == nil || sn.TS.After(best.TS)) {
			cp := sn
			best = &cp
		}
	}
	if best == nil {
		return domain.ProcessSnapshot{}, fmt.Errorf("process snapshot: %w", domain.ErrNotFound)
	}
	return *best, nil
}
