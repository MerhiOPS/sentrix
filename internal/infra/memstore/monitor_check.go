package memstore

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/uptime"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// msTime and msDur round as SQLite's millisecond columns do.
func msTime(t time.Time) time.Time        { return time.UnixMilli(t.UnixMilli()).UTC() }
func msDur(d time.Duration) time.Duration { return d.Truncate(time.Millisecond) }

func msOptTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := msTime(*t)
	return &v
}

// RecordCheck implements uptime.StateRepo, with the same all-or-nothing
// rule as SQLite: every check happens before anything is written.
func (s *Store) RecordCheck(_ context.Context, sc domain.Scope, rec uptime.CheckRecord) ([]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return nil, err
	}
	m, ok := s.mons.monitors[rec.Monitor.ID]
	if !ok || m.tenant != tid || m.st.Revision != rec.Revision || m.m.Paused {
		return nil, uptime.ErrStale
	}
	r := rec.Result
	r.At, r.CertExpiry = msTime(r.At), msOptTime(r.CertExpiry)
	r.Latency, r.TTFB = msDur(r.Latency), msDur(r.TTFB)
	if r.Outcome == domain.CheckUnknown {
		r.Latency = 0
	}
	m.results = append(m.results, r)
	slices.SortFunc(m.results, func(a, b domain.CheckResult) int { return a.At.Compare(b.At) })
	m.rollup(r, rec.Step)
	s.meterCheck(tid, utcDay(r.At))
	m.applyIncident(&s.mons, rec.Step)
	ids := make([]int64, 0, len(rec.Step.Events))
	for _, ev := range rec.Step.Events {
		s.mons.nextEvent++
		ev.ID, ev.Monitor, ev.At, ev.CertExpiry = s.mons.nextEvent, m.m.ID, msTime(ev.At), msOptTime(ev.CertExpiry)
		ev.DownFor, ev.Outcome, ev.Delivery = msDur(ev.DownFor), "", ""
		if ev.Kind != domain.EventUp {
			ev.DownFor = 0
		}
		m.events = append(m.events, ev)
		ids = append(ids, ev.ID)
	}
	next := rec.Step.Next
	next.Monitor, next.Revision = m.m.ID, m.st.Revision+1
	next.Since, next.LastCheck, next.CertExpiry = msTime(next.Since), msTime(next.LastCheck), msOptTime(next.CertExpiry)
	if !next.FirstFailure.IsZero() {
		next.FirstFailure = msTime(next.FirstFailure)
	}
	m.st = next
	return ids, nil
}

func (m *mon) hour(t time.Time) *domain.MonitorHour {
	h := t.UTC().Truncate(time.Hour)
	if row, ok := m.hours[h.UnixMilli()]; ok {
		return row
	}
	row := &domain.MonitorHour{Monitor: m.m.ID, Hour: h}
	m.hours[h.UnixMilli()] = row
	return row
}

func (m *mon) rollup(r domain.CheckResult, st domain.MonitorStep) {
	for _, sl := range domain.HourSlices(st.Covered) {
		h := m.hour(sl.Hour)
		if st.CoveredDown {
			h.Down += msDur(sl.D)
		} else {
			h.Up += msDur(sl.D)
		}
	}
	if st.Reclassify != nil {
		for _, sl := range domain.HourSlices(*st.Reclassify) {
			h := m.hour(sl.Hour)
			h.Up, h.Down = max(h.Up-msDur(sl.D), 0), h.Down+msDur(sl.D)
		}
	}
	h := m.hour(r.At)
	h.Checks++
	if r.Outcome == domain.CheckDown {
		h.Failures++
	}
	if r.Outcome != domain.CheckUnknown && r.Latency > 0 {
		h.LatencySum += r.Latency
		h.LatencyN++
		h.LatencyMax = max(h.LatencyMax, r.Latency)
	}
}

func (m *mon) applyIncident(d *monitorData, st domain.MonitorStep) {
	if o := st.Open; o != nil {
		d.nextIncident++
		m.incidents = append(m.incidents, domain.Incident{ID: d.nextIncident, Monitor: m.m.ID, Start: msTime(o.Start),
			Class: o.Class, Failures: o.Failures})
	}
	for i := range m.incidents {
		if m.incidents[i].End != nil {
			continue
		}
		if st.Failure {
			m.incidents[i].Failures++
		}
		if st.Close != nil {
			end := msTime(*st.Close)
			m.incidents[i].End = &end
		}
	}
}

// meterCheck mirrors SQLite's usage_daily.checks. Caller holds mu.
func (s *Store) meterCheck(t domain.TenantID, day int64) {
	k := usageKey{t, day}
	du, ok := s.meter.daily[k]
	if !ok {
		du = &dayUsage{}
		s.meter.daily[k] = du
	}
	du.checks++
}

// MonitorStates implements uptime.StateRepo.
func (s *Store) MonitorStates(_ context.Context, sc domain.Scope) ([]domain.MonitorState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return nil, err
	}
	var out []domain.MonitorState
	for _, m := range s.mons.monitors {
		if m.tenant == tid {
			out = append(out, m.st)
		}
	}
	slices.SortFunc(out, func(a, b domain.MonitorState) int { return int(a.Monitor - b.Monitor) })
	return out, nil
}

// SetMonitorDelivery implements uptime.StateRepo.
func (s *Store) SetMonitorDelivery(_ context.Context, sc domain.Scope, event int64, outcome, delivery string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return err
	}
	for _, m := range s.mons.monitors {
		if m.tenant != tid {
			continue
		}
		for i := range m.events {
			if m.events[i].ID == event {
				m.events[i].Outcome, m.events[i].Delivery = outcome, delivery
				return nil
			}
		}
	}
	return fmt.Errorf("monitor event: %w", domain.ErrNotFound)
}

// ListMonitorEvents implements uptime.StateRepo.
func (s *Store) ListMonitorEvents(_ context.Context, sc domain.Scope, monitor int64, limit int) ([]domain.MonitorEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return nil, err
	}
	var out []domain.MonitorEvent
	for _, m := range s.mons.monitors {
		if m.tenant == tid && (monitor == 0 || m.m.ID == monitor) {
			out = append(out, m.events...)
		}
	}
	slices.SortFunc(out, func(a, b domain.MonitorEvent) int { return int(b.ID - a.ID) })
	return out[:min(limit, len(out))], nil
}
