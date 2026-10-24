package memstore

import (
	"context"
	"fmt"
	"slices"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// alertData holds alerting state. Every row records its tenant; every
// lookup checks it.
type alertData struct {
	next     int64
	rules    map[int64]owned[domain.AlertRule]
	channels map[int64]owned[domain.Channel]
	silences map[int64]owned[domain.Silence]
	states   map[[2]any]owned[domain.RuleHostState] // by (rule id, host id)
	events   []owned[domain.AlertEvent]
}

type owned[T any] struct {
	tenant domain.TenantID
	v      T
}

func newAlertData() alertData {
	return alertData{rules: map[int64]owned[domain.AlertRule]{}, channels: map[int64]owned[domain.Channel]{},
		silences: map[int64]owned[domain.Silence]{}, states: map[[2]any]owned[domain.RuleHostState]{}}
}

// ListRules implements alerting.RuleRepo.
func (s *Store) ListRules(_ context.Context, sc domain.Scope) ([]domain.AlertRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return nil, err
	}
	return listOwned(s.alerts.rules, tid, func(r domain.AlertRule) int64 { return r.ID }), nil
}

// CreateRule implements alerting.RuleRepo.
func (s *Store) CreateRule(_ context.Context, sc domain.Scope, r domain.AlertRule) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.checkRuleRefs(sc, r)
	if err != nil {
		return 0, err
	}
	s.alerts.next++
	r.ID = s.alerts.next
	s.alerts.rules[r.ID] = owned[domain.AlertRule]{tid, cloneRule(r)}
	return r.ID, nil
}

// UpdateRule implements alerting.RuleRepo.
func (s *Store) UpdateRule(_ context.Context, sc domain.Scope, r domain.AlertRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.checkRuleRefs(sc, r)
	if err != nil {
		return err
	}
	if cur, ok := s.alerts.rules[r.ID]; !ok || cur.tenant != tid {
		return fmt.Errorf("alert rule: %w", domain.ErrNotFound)
	}
	s.alerts.rules[r.ID] = owned[domain.AlertRule]{tid, cloneRule(r)}
	return nil
}

// DeleteRule implements alerting.RuleRepo, cascading like the schema.
func (s *Store) DeleteRule(_ context.Context, sc domain.Scope, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return err
	}
	if cur, ok := s.alerts.rules[id]; !ok || cur.tenant != tid {
		return fmt.Errorf("alert rule: %w", domain.ErrNotFound)
	}
	delete(s.alerts.rules, id)
	for k, st := range s.alerts.states {
		if st.v.Rule == id {
			delete(s.alerts.states, k)
		}
	}
	s.alerts.events = slices.DeleteFunc(s.alerts.events, func(e owned[domain.AlertEvent]) bool { return e.v.Rule == id })
	for k, sil := range s.alerts.silences {
		if sil.v.Rule != nil && *sil.v.Rule == id {
			delete(s.alerts.silences, k)
		}
	}
	return nil
}

// checkRuleRefs resolves the scope and checks every host and channel the
// rule names belongs to it. Caller holds mu.
func (s *Store) checkRuleRefs(sc domain.Scope, r domain.AlertRule) (domain.TenantID, error) {
	tid, err := s.tenantOf(sc)
	if err != nil {
		return tid, err
	}
	for _, h := range r.Hosts {
		if _, err := s.hostIn(sc, h); err != nil {
			return tid, err
		}
	}
	for _, c := range r.Channels {
		if cur, ok := s.alerts.channels[c]; !ok || cur.tenant != tid {
			return tid, fmt.Errorf("channel: %w", domain.ErrNotFound)
		}
	}
	return tid, nil
}

func cloneRule(r domain.AlertRule) domain.AlertRule {
	r.Hosts = slices.Clone(r.Hosts)
	r.Channels = slices.Clone(r.Channels)
	return r
}

// ListChannels implements alerting.ChannelRepo.
func (s *Store) ListChannels(_ context.Context, sc domain.Scope) ([]domain.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return nil, err
	}
	return listOwned(s.alerts.channels, tid, func(c domain.Channel) int64 { return c.ID }), nil
}

// CreateChannel implements alerting.ChannelRepo.
func (s *Store) CreateChannel(_ context.Context, sc domain.Scope, c domain.Channel) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return 0, err
	}
	s.alerts.next++
	c.ID = s.alerts.next
	s.alerts.channels[c.ID] = owned[domain.Channel]{tid, c}
	return c.ID, nil
}

// UpdateChannel implements alerting.ChannelRepo.
func (s *Store) UpdateChannel(_ context.Context, sc domain.Scope, c domain.Channel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return err
	}
	if cur, ok := s.alerts.channels[c.ID]; !ok || cur.tenant != tid {
		return fmt.Errorf("channel: %w", domain.ErrNotFound)
	}
	s.alerts.channels[c.ID] = owned[domain.Channel]{tid, c}
	return nil
}

// DeleteChannel implements alerting.ChannelRepo; rules stop naming it.
func (s *Store) DeleteChannel(_ context.Context, sc domain.Scope, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return err
	}
	if cur, ok := s.alerts.channels[id]; !ok || cur.tenant != tid {
		return fmt.Errorf("channel: %w", domain.ErrNotFound)
	}
	delete(s.alerts.channels, id)
	for k, r := range s.alerts.rules {
		r.v.Channels = slices.DeleteFunc(r.v.Channels, func(c int64) bool { return c == id })
		s.alerts.rules[k] = r
	}
	return nil
}

// listOwned returns the tenant's values ordered by id.
func listOwned[T any](m map[int64]owned[T], tid domain.TenantID, id func(T) int64) []T {
	var out []T
	for _, o := range m {
		if o.tenant == tid {
			out = append(out, o.v)
		}
	}
	slices.SortFunc(out, func(a, b T) int { return int(id(a) - id(b)) })
	return out
}
