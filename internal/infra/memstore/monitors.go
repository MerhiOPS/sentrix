package memstore

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// monitorData holds uptime data. Every monitor records its tenant; every
// lookup checks it.
type monitorData struct {
	next, nextEvent, nextIncident int64
	monitors                      map[int64]*mon
}

type mon struct {
	tenant    domain.TenantID
	m         domain.Monitor
	st        domain.MonitorState
	results   []domain.CheckResult // by At
	hours     map[int64]*domain.MonitorHour
	incidents []domain.Incident
	events    []domain.MonitorEvent
}

func newMonitorData() monitorData { return monitorData{monitors: map[int64]*mon{}} }

// monitorIn returns the monitor only if it belongs to the scope's tenant.
// Caller holds mu.
func (s *Store) monitorIn(sc domain.Scope, id int64) (*mon, domain.TenantID, error) {
	tid, err := s.tenantOf(sc)
	if err != nil {
		return nil, tid, err
	}
	m, ok := s.mons.monitors[id]
	if !ok || m.tenant != tid {
		return nil, tid, fmt.Errorf("monitor: %w", domain.ErrNotFound)
	}
	return m, tid, nil
}

func cloneMonitor(m domain.Monitor) domain.Monitor {
	m.AcceptStatus = slices.Clone(m.AcceptStatus)
	m.Channels = slices.Clone(m.Channels)
	m.CreatedAt = m.CreatedAt.Truncate(time.Millisecond)
	return m
}

// ListMonitors implements uptime.MonitorRepo.
func (s *Store) ListMonitors(_ context.Context, sc domain.Scope) ([]domain.Monitor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return nil, err
	}
	var out []domain.Monitor
	for _, m := range s.mons.monitors {
		if m.tenant == tid {
			out = append(out, cloneMonitor(m.m))
		}
	}
	slices.SortFunc(out, func(a, b domain.Monitor) int { return int(a.ID - b.ID) })
	return out, nil
}

// GetMonitor implements uptime.MonitorRepo.
func (s *Store) GetMonitor(_ context.Context, sc domain.Scope, id int64) (domain.Monitor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _, err := s.monitorIn(sc, id)
	if err != nil {
		return domain.Monitor{}, err
	}
	return cloneMonitor(m.m), nil
}

// checkChannels refuses channels the tenant does not own. Caller holds mu.
func (s *Store) checkChannels(tid domain.TenantID, ids []int64) error {
	for _, c := range ids {
		if ch, ok := s.alerts.channels[c]; !ok || ch.tenant != tid {
			return fmt.Errorf("channel: %w", domain.ErrNotFound)
		}
	}
	return nil
}

// CreateMonitor implements uptime.MonitorRepo.
func (s *Store) CreateMonitor(_ context.Context, sc domain.Scope, m domain.Monitor, maxMonitors int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return 0, err
	}
	if err := s.checkChannels(tid, m.Channels); err != nil {
		return 0, err
	}
	n := 0
	for _, o := range s.mons.monitors {
		if o.tenant == tid {
			n++
		}
	}
	if n >= maxMonitors {
		return 0, fmt.Errorf("%w: the plan allows %d monitors", domain.ErrQuotaExceeded, maxMonitors)
	}
	s.mons.next++
	m.ID, m.Paused = s.mons.next, false
	m.Channels = dedupe(m.Channels)
	s.mons.monitors[m.ID] = &mon{tenant: tid, m: cloneMonitor(m), hours: map[int64]*domain.MonitorHour{},
		st: domain.MonitorState{Monitor: m.ID, Status: domain.MonitorPending, Since: m.CreatedAt.Truncate(time.Millisecond)}}
	return m.ID, nil
}

func dedupe(ids []int64) []int64 {
	out := slices.Clone(ids)
	slices.Sort(out)
	return slices.Compact(out)
}

// UpdateMonitor implements uptime.MonitorRepo.
func (s *Store) UpdateMonitor(_ context.Context, sc domain.Scope, m domain.Monitor, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, tid, err := s.monitorIn(sc, m.ID)
	if err != nil {
		return err
	}
	if err := s.checkChannels(tid, m.Channels); err != nil {
		return err
	}
	retarget := cur.m.Kind != m.Kind || cur.m.Target != m.Target
	m.Paused, m.CreatedAt = cur.m.Paused, cur.m.CreatedAt
	m.Channels = dedupe(m.Channels)
	cur.m = cloneMonitor(m)
	cur.st.Revision++
	if retarget && !cur.m.Paused {
		cur.reset(domain.MonitorPending, at)
	}
	return nil
}

// reset starts the state over and ends any open incident at at.
func (m *mon) reset(status domain.MonitorStatus, at time.Time) {
	at = at.Truncate(time.Millisecond)
	for i := range m.incidents {
		if m.incidents[i].End == nil {
			end := at
			m.incidents[i].End = &end
		}
	}
	m.st.Status, m.st.Since, m.st.Failures, m.st.FirstFailure = status, at, 0, time.Time{}
	m.st.Revision++
}

// DeleteMonitor implements uptime.MonitorRepo, cascading like the schema.
func (s *Store) DeleteMonitor(_ context.Context, sc domain.Scope, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, _, err := s.monitorIn(sc, id); err != nil {
		return err
	}
	delete(s.mons.monitors, id)
	return nil
}

// SetPaused implements uptime.MonitorRepo.
func (s *Store) SetPaused(_ context.Context, sc domain.Scope, id int64, paused bool, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, _, err := s.monitorIn(sc, id)
	if err != nil || m.m.Paused == paused {
		return err
	}
	m.m.Paused = paused
	if paused {
		m.reset(domain.MonitorPaused, at)
	} else {
		m.reset(domain.MonitorPending, at)
	}
	return nil
}

// dropChannelFromMonitors mirrors the schema's cascade when a channel is
// deleted. Caller holds mu.
func (s *Store) dropChannelFromMonitors(channel int64) {
	for _, m := range s.mons.monitors {
		m.m.Channels = slices.DeleteFunc(m.m.Channels, func(c int64) bool { return c == channel })
	}
}
