package ingest

import (
	"sort"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// normalizeDetails validates detail samples against the catalog, clamps and
// snaps them like core samples, dedupes per (series, slot) keeping the last,
// and sorts by time. It returns the survivors and how many were rejected.
func normalizeDetails(in []domain.DetailSample, interval time.Duration, w Window, now time.Time) ([]domain.DetailSample, int) {
	type slot struct {
		key domain.SeriesKey
		ms  int64
	}
	at := make(map[slot]int, len(in))
	out := make([]domain.DetailSample, 0, len(in))
	rejected := 0
	for _, d := range in {
		if domain.CheckDetail(d) != nil || !w.accept(d.TS, now) {
			rejected++
			continue
		}
		d.TS = domain.SnapToGrid(d.TS, interval)
		k := slot{domain.KeyFor(d.Metric, d.Labels), d.TS.UnixMilli()}
		if i, dup := at[k]; dup {
			out[i] = d
			continue
		}
		at[k] = len(out)
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TS.Before(out[j].TS) })
	return out, rejected
}

// foldDetailCounters applies reset folding to detail counters, keyed per
// series in the same CounterState as the core counters. Gauges pass
// through. state is not modified; the new state is returned.
func foldDetailCounters(state CounterState, in []domain.DetailSample) ([]domain.DetailSample, CounterState) {
	next := state.clone()
	out := make([]domain.DetailSample, len(in))
	copy(out, in)
	for i := range out {
		d := &out[i]
		if d.Kind != domain.KindCounter {
			continue
		}
		key := Counter(domain.KeyFor(d.Metric, d.Labels).CounterKey())
		raw := uint64(d.Value)
		cv, seen := next[key]
		if seen && raw < cv.LastRaw {
			cv.Offset += cv.LastRaw
		}
		cv.LastRaw = raw
		next[key] = cv
		d.Value = float64(raw + cv.Offset)
	}
	return out, next
}

// normalizeSnapshots validates and clamps process snapshots, dedupes by
// timestamp (to the second) and sorts them.
func normalizeSnapshots(in []domain.ProcessSnapshot, w Window, now time.Time) ([]domain.ProcessSnapshot, int) {
	seen := map[int64]int{}
	out := make([]domain.ProcessSnapshot, 0, len(in))
	rejected := 0
	for _, s := range in {
		if s.Validate() != nil || !w.accept(s.TS, now) {
			rejected++
			continue
		}
		s.TS = s.TS.Truncate(time.Second).UTC()
		if i, dup := seen[s.TS.Unix()]; dup {
			out[i] = s
			continue
		}
		seen[s.TS.Unix()] = len(out)
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TS.Before(out[j].TS) })
	return out, rejected
}
