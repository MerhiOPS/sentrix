package memstore

import (
	"context"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// meter holds write-path metering, like SQLite's usage tables.
type meter struct {
	plans    map[domain.TenantID]domain.Plan
	daily    map[usageKey]*dayUsage
	hostDays map[hostDay]bool
}

type dayUsage struct {
	samples, logBytes int64
	hosts             int
}

type hostDay struct {
	host domain.HostID
	day  int64
}

func newMeter() meter {
	return meter{plans: map[domain.TenantID]domain.Plan{}, daily: map[usageKey]*dayUsage{}, hostDays: map[hostDay]bool{}}
}

// planOf returns the tenant's plan (the default until set). Caller holds mu.
func (s *Store) planOf(t domain.TenantID) domain.Plan {
	if p, ok := s.meter.plans[t]; ok {
		return p
	}
	p := domain.DefaultPlan
	if l, ok := s.logLimits[t]; ok {
		p.LogBytesPerDay = l
	}
	return p
}

// SetPlan implements identity.PlanRepo.
func (s *Store) SetPlan(_ context.Context, ops domain.OpsScope, t domain.TenantID, p domain.Plan) error {
	if _, err := ops.Reason(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tenants[t]; !ok {
		return fmt.Errorf("tenant: %w", domain.ErrNotFound)
	}
	s.meter.plans[t] = p
	s.logLimits[t] = p.LogBytesPerDay
	return nil
}

// GetPlan implements identity.PlanRepo.
func (s *Store) GetPlan(_ context.Context, sc domain.Scope) (domain.Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return domain.Plan{}, err
	}
	return s.planOf(tid), nil
}

// GetUsage implements identity.PlanRepo.
func (s *Store) GetUsage(_ context.Context, sc domain.Scope, now time.Time) (domain.Usage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return domain.Usage{}, err
	}
	u := domain.Usage{Hosts: s.enabledHosts(tid), LogBytesToday: s.logUsage[usageKey{tid, utcDay(now)}]}
	first := utcDay(time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC))
	for d := first; d <= utcDay(now); d++ {
		if du, ok := s.meter.daily[usageKey{tid, d}]; ok {
			u.MonthSamples += du.samples
			u.MonthLogBytes += du.logBytes
			u.MonthPeakHosts = max(u.MonthPeakHosts, du.hosts)
		}
	}
	return u, nil
}

// meterBatch mirrors SQLite's: after the replay gate. Caller holds mu.
func (s *Store) meterBatch(h *host, day int64, samples int, logBytes int64) {
	k := usageKey{h.h.Tenant, day}
	du, ok := s.meter.daily[k]
	if !ok {
		du = &dayUsage{}
		s.meter.daily[k] = du
	}
	du.samples += int64(samples)
	du.logBytes += logBytes
	if hd := (hostDay{h.h.ID, day}); !s.meter.hostDays[hd] {
		s.meter.hostDays[hd] = true
		du.hosts++
	}
}

func (s *Store) enabledHosts(t domain.TenantID) int {
	n := 0
	for _, h := range s.hosts {
		if h.h.Tenant == t && h.h.Enabled() {
			n++
		}
	}
	return n
}
