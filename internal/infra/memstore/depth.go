package memstore

import (
	"fmt"
	"math"

	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// defaultSeriesLimit mirrors the SQLite column default.
const defaultSeriesLimit = 10000

type mseries struct {
	s      domain.Series
	scale  int64
	points map[int64]float64 // ts ms → value at storage precision
}

func seriesKeyOf(metric, hash string) string { return metric + "|" + hash }

// tenantSeriesCount counts a tenant's series. Caller holds mu.
func (s *Store) tenantSeriesCount(t domain.TenantID) int {
	n := 0
	for _, h := range s.hosts {
		if h.h.Tenant == t {
			n += len(h.series)
		}
	}
	return n
}

// applyDetails mirrors the SQLite adapter: series within the ceiling, values
// at fixed-point precision, conflicts counted. Caller holds mu and has
// passed every fallible check; this only mutates.
func (s *Store) applyDetails(h *host, details []domain.DetailSample, res *ingest.CommitResult) {
	room := s.seriesLimit(h.h.Tenant) - s.tenantSeriesCount(h.h.Tenant)
	for _, d := range details {
		m, _ := domain.LookupMetric(d.Metric)
		k := domain.KeyFor(d.Metric, d.Labels)
		ms, ok := h.series[seriesKeyOf(k.Metric, k.LabelsHash)]
		if !ok {
			if room <= 0 {
				res.SeriesRefused++
				continue
			}
			room--
			s.nextSeries++
			ms = &mseries{s: domain.Series{ID: s.nextSeries, Host: h.h.ID, Key: k, Kind: m.Kind, Labels: d.Labels},
				scale: m.Scale, points: map[int64]float64{}}
			h.series[seriesKeyOf(k.Metric, k.LabelsHash)] = ms
		}
		ts := d.TS.UnixMilli()
		if _, dup := ms.points[ts]; dup {
			res.Conflicts++
			continue
		}
		ms.points[ts] = math.Round(d.Value*float64(m.Scale)) / float64(m.Scale)
	}
}

func (s *Store) applySnapshots(h *host, snaps []domain.ProcessSnapshot, res *ingest.CommitResult) {
	for _, sn := range snaps {
		ts := sn.TS.UnixMilli()
		if _, dup := h.snaps[ts]; dup {
			res.Conflicts++
			continue
		}
		cp := domain.ProcessSnapshot{TS: sn.TS, Procs: make([]domain.Process, len(sn.Procs))}
		for i, p := range sn.Procs {
			cp.Procs[i] = domain.Process{Name: p.Name, CPUPercent: math.Round(p.CPUPercent*100) / 100, RSS: p.RSS}
		}
		h.snaps[ts] = cp
	}
}

func (s *Store) seriesLimit(t domain.TenantID) int {
	if l, ok := s.limits[t]; ok {
		return l
	}
	return defaultSeriesLimit
}

// fitsDetails mirrors the SQLite range checks before anything is mutated.
func fitsDetails(p ingest.Prepared) error {
	for _, d := range p.Details {
		if _, ok := domain.LookupMetric(d.Metric); !ok {
			return fmt.Errorf("detail metric %q passed validation but is not in the catalog", d.Metric)
		}
	}
	return nil
}
