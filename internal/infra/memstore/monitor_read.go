package memstore

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/retention"
	"github.com/MerhiOPS/sentrix/internal/app/uptime"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// MonitorResults implements uptime.HistoryReader.
func (s *Store) MonitorResults(_ context.Context, sc domain.Scope, monitor int64, from, to time.Time, limit int) ([]domain.CheckResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _, err := s.monitorIn(sc, monitor)
	if err != nil {
		return nil, nil // like SQLite: an unknown monitor has no results
	}
	var out []domain.CheckResult
	for _, r := range m.results {
		if !r.At.Before(from) && r.At.Before(to) {
			out = append(out, r)
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return slices.Clone(out), nil
}

// MonitorHours implements uptime.HistoryReader.
func (s *Store) MonitorHours(_ context.Context, sc domain.Scope, monitor int64, from, to time.Time) ([]domain.MonitorHour, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return nil, err
	}
	var out []domain.MonitorHour
	for _, m := range s.mons.monitors {
		if m.tenant != tid || (monitor != 0 && m.m.ID != monitor) {
			continue
		}
		for _, h := range m.hours {
			if !h.Hour.Before(from) && h.Hour.Before(to) {
				out = append(out, *h)
			}
		}
	}
	slices.SortFunc(out, func(a, b domain.MonitorHour) int {
		if c := a.Hour.Compare(b.Hour); c != 0 {
			return c
		}
		return int(a.Monitor - b.Monitor)
	})
	return out, nil
}

// MonitorIncidents implements uptime.HistoryReader.
func (s *Store) MonitorIncidents(_ context.Context, sc domain.Scope, monitor int64, limit int) ([]domain.Incident, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _, err := s.monitorIn(sc, monitor)
	if err != nil {
		return nil, nil
	}
	out := slices.Clone(m.incidents)
	slices.SortFunc(out, func(a, b domain.Incident) int {
		if c := b.Start.Compare(a.Start); c != 0 {
			return c
		}
		return int(b.ID - a.ID)
	})
	return out[:min(limit, len(out))], nil
}

// ScheduledMonitors implements uptime.ScheduleReader.
func (s *Store) ScheduledMonitors(_ context.Context, ops domain.OpsScope) ([]uptime.Scheduled, error) {
	if _, err := ops.Reason(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []uptime.Scheduled
	for _, m := range s.mons.monitors {
		if !m.m.Paused {
			out = append(out, uptime.Scheduled{Tenant: m.tenant, Monitor: cloneMonitor(m.m), State: m.st})
		}
	}
	slices.SortFunc(out, func(a, b uptime.Scheduled) int { return int(a.Monitor.ID - b.Monitor.ID) })
	return out, nil
}

// FailUndeliveredMonitorEvents implements uptime.Sweeper.
func (s *Store) FailUndeliveredMonitorEvents(_ context.Context, ops domain.OpsScope, reason string) (int, error) {
	if _, err := ops.Reason(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.mons.monitors {
		for i := range m.events {
			if m.events[i].Notify && m.events[i].Outcome == "" {
				m.events[i].Outcome, m.events[i].Delivery = "failed", reason
				n++
			}
		}
	}
	return n, nil
}

// MonitorRetentionTargets implements retention.MonitorStore.
func (s *Store) MonitorRetentionTargets(_ context.Context, ops domain.OpsScope) ([]retention.MonitorTarget, error) {
	if _, err := ops.Reason(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []retention.MonitorTarget
	for _, m := range s.mons.monitors {
		out = append(out, retention.MonitorTarget{Tenant: m.tenant, Monitor: m.m.ID, Plan: s.planOf(m.tenant)})
	}
	slices.SortFunc(out, func(a, b retention.MonitorTarget) int { return int(a.Monitor - b.Monitor) })
	return out, nil
}

// PruneMonitor implements retention.MonitorStore.
func (s *Store) PruneMonitor(_ context.Context, ops domain.OpsScope, t retention.MonitorTarget, k domain.DataKind,
	before time.Time, limit int) (int, error) {
	m, err := s.monitorTarget(ops, t)
	if err != nil || m == nil {
		return 0, err
	}
	defer s.mu.Unlock()
	n := 0
	switch k {
	case domain.DataMonitorResults:
		m.results = slices.DeleteFunc(m.results, func(r domain.CheckResult) bool {
			drop := n < limit && r.At.Before(before)
			n += boolCount(drop)
			return drop
		})
	case domain.DataMonitorHourly:
		keys := slices.Sorted(maps.Keys(m.hours)) // oldest first, like SQLite
		for _, key := range keys {
			if n < limit && m.hours[key].Hour.Before(before) {
				delete(m.hours, key)
				n++
			}
		}
	case domain.DataMonitorIncidents:
		m.incidents = slices.DeleteFunc(m.incidents, func(in domain.Incident) bool {
			drop := n < limit && in.End != nil && in.End.Before(before)
			n += boolCount(drop)
			return drop
		})
		events := 0
		m.events = slices.DeleteFunc(m.events, func(ev domain.MonitorEvent) bool {
			drop := events < limit && ev.At.Before(before)
			events += boolCount(drop)
			return drop
		})
		n += events
	default:
		return 0, errKind(k)
	}
	return n, nil
}

// OldestMonitor implements retention.MonitorStore.
func (s *Store) OldestMonitor(_ context.Context, ops domain.OpsScope, t retention.MonitorTarget, k domain.DataKind) (time.Time, bool, error) {
	m, err := s.monitorTarget(ops, t)
	if err != nil || m == nil {
		return time.Time{}, false, err
	}
	defer s.mu.Unlock()
	var times []time.Time
	switch k {
	case domain.DataMonitorResults:
		for _, r := range m.results {
			times = append(times, r.At)
		}
	case domain.DataMonitorHourly:
		for _, h := range m.hours {
			times = append(times, h.Hour)
		}
	case domain.DataMonitorIncidents:
		for _, in := range m.incidents {
			if in.End != nil {
				times = append(times, *in.End)
			}
		}
		for _, ev := range m.events {
			times = append(times, ev.At)
		}
	default:
		return time.Time{}, false, errKind(k)
	}
	if len(times) == 0 {
		return time.Time{}, false, nil
	}
	return slices.MinFunc(times, time.Time.Compare), true, nil
}

// monitorTarget locks mu and returns the target's monitor (nil, unlocked,
// if it no longer exists).
func (s *Store) monitorTarget(ops domain.OpsScope, t retention.MonitorTarget) (*mon, error) {
	if _, err := ops.Reason(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	m, ok := s.mons.monitors[t.Monitor]
	if !ok || m.tenant != t.Tenant {
		s.mu.Unlock()
		return nil, nil
	}
	return m, nil
}

func boolCount(b bool) int {
	if b {
		return 1
	}
	return 0
}

func errKind(k domain.DataKind) error { return fmt.Errorf("%w: data kind %q", domain.ErrInvalid, k) }
