package memstore

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// ListSilences implements alerting.SilenceRepo.
func (s *Store) ListSilences(_ context.Context, sc domain.Scope) ([]domain.Silence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return nil, err
	}
	return listOwned(s.alerts.silences, tid, func(x domain.Silence) int64 { return x.ID }), nil
}

// CreateSilence implements alerting.SilenceRepo.
func (s *Store) CreateSilence(_ context.Context, sc domain.Scope, sil domain.Silence) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return 0, err
	}
	if sil.Rule != nil {
		if r, ok := s.alerts.rules[*sil.Rule]; !ok || r.tenant != tid {
			return 0, fmt.Errorf("alert rule: %w", domain.ErrNotFound)
		}
	}
	if sil.Host != nil {
		if _, err := s.hostIn(sc, *sil.Host); err != nil {
			return 0, err
		}
	}
	s.alerts.next++
	sil.ID = s.alerts.next
	s.alerts.silences[sil.ID] = owned[domain.Silence]{tid, sil}
	return sil.ID, nil
}

// DeleteSilence implements alerting.SilenceRepo.
func (s *Store) DeleteSilence(_ context.Context, sc domain.Scope, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return err
	}
	if cur, ok := s.alerts.silences[id]; !ok || cur.tenant != tid {
		return fmt.Errorf("silence: %w", domain.ErrNotFound)
	}
	delete(s.alerts.silences, id)
	return nil
}

// AlertStates implements alerting.StateRepo.
func (s *Store) AlertStates(_ context.Context, sc domain.Scope) ([]domain.RuleHostState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return nil, err
	}
	var out []domain.RuleHostState
	for _, st := range s.alerts.states {
		if st.tenant == tid {
			out = append(out, st.v)
		}
	}
	slices.SortFunc(out, func(a, b domain.RuleHostState) int { return int(a.Rule - b.Rule) })
	return out, nil
}

// RecordTransition implements alerting.StateRepo.
func (s *Store) RecordTransition(_ context.Context, sc domain.Scope, st domain.RuleHostState, ev *domain.AlertEvent) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return 0, err
	}
	if _, err := s.hostIn(sc, st.Host); err != nil {
		return 0, err
	}
	if r, ok := s.alerts.rules[st.Rule]; !ok || r.tenant != tid {
		return 0, fmt.Errorf("alert rule: %w", domain.ErrNotFound)
	}
	var id int64
	if ev != nil {
		s.alerts.next++
		e := *ev
		e.ID, e.Rule, e.Host, e.Outcome, e.Delivery = s.alerts.next, st.Rule, st.Host, "", ""
		id = e.ID
		s.alerts.events = append(s.alerts.events, owned[domain.AlertEvent]{tid, e})
	}
	s.alerts.states[[2]any{st.Rule, st.Host}] = owned[domain.RuleHostState]{tid, st}
	return id, nil
}

// SetDelivery implements alerting.StateRepo.
func (s *Store) SetDelivery(_ context.Context, sc domain.Scope, event int64, outcome, delivery string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return err
	}
	for i, e := range s.alerts.events {
		if e.v.ID == event && e.tenant == tid {
			s.alerts.events[i].v.Outcome, s.alerts.events[i].v.Delivery = outcome, delivery
			return nil
		}
	}
	return fmt.Errorf("alert event: %w", domain.ErrNotFound)
}

// ListEvents implements alerting.StateRepo, newest first.
func (s *Store) ListEvents(_ context.Context, sc domain.Scope, limit int) ([]domain.AlertEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return nil, err
	}
	var out []domain.AlertEvent
	for i := len(s.alerts.events) - 1; i >= 0 && len(out) < limit; i-- {
		if e := s.alerts.events[i]; e.tenant == tid {
			out = append(out, e.v)
		}
	}
	return out, nil
}

// TenantBuckets implements alerting.BucketReader.
func (s *Store) TenantBuckets(ctx context.Context, sc domain.Scope, start time.Time) (map[domain.HostID]query.RollupBucket, error) {
	hosts, err := s.ListHosts(ctx, sc)
	if err != nil {
		return nil, err
	}
	out := map[domain.HostID]query.RollupBucket{}
	for _, h := range hosts {
		bs, err := s.Rollups(ctx, sc, h.ID, query.Res1m, start, start.Add(time.Minute))
		if err != nil {
			return nil, err
		}
		if len(bs) == 1 {
			out[h.ID] = bs[0]
		}
	}
	return out, nil
}

// ListTenants implements alerting.TenantLister.
func (s *Store) ListTenants(_ context.Context, ops domain.OpsScope) ([]domain.TenantID, error) {
	if _, err := ops.Reason(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.TenantID
	for id := range s.tenants {
		out = append(out, id)
	}
	return out, nil
}

// SetHostDisabled implements identity.HostRepo.
func (s *Store) SetHostDisabled(_ context.Context, sc domain.Scope, id domain.HostID, at *time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, err := s.hostIn(sc, id)
	if err != nil {
		return err
	}
	if at != nil {
		t := at.UTC().Truncate(time.Millisecond)
		at = &t
	}
	h.h.DisabledAt = at
	return nil
}
