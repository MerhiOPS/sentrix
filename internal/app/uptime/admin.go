package uptime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// PlanReader reads a tenant's plan.
type PlanReader interface {
	GetPlan(ctx context.Context, s domain.Scope) (domain.Plan, error)
}

// Scheduling is the running scheduler, told about changes.
type Scheduling interface {
	Refresh()
	CheckNow(id int64) bool
}

// AdminDeps are the admin use case's ports.
type AdminDeps struct {
	Monitors MonitorRepo
	States   StateRepo
	History  HistoryReader
	Plans    PlanReader
	Sched    Scheduling
	// PingAvailable reports why ping monitors cannot run here (nil: they can).
	PingAvailable func() error
	Now           func() time.Time
}

// CheckNowEvery is how often one monitor may be checked on demand.
const CheckNowEvery = 10 * time.Second

// Admin manages monitors and reads their history.
type Admin struct {
	d AdminDeps

	mu      sync.Mutex
	lastNow map[int64]time.Time
}

// NewAdmin builds the use case.
func NewAdmin(d AdminDeps) *Admin { return &Admin{d: d, lastNow: map[int64]time.Time{}} }

// Create validates m against the plan and stores it.
func (a *Admin) Create(ctx context.Context, s domain.Scope, m domain.Monitor) (domain.Monitor, error) {
	plan, err := a.d.Plans.GetPlan(ctx, s)
	if err != nil {
		return domain.Monitor{}, err
	}
	if err := a.validate(m, plan); err != nil {
		return domain.Monitor{}, err
	}
	m.CreatedAt, m.Paused = a.d.Now().UTC(), false
	if m.ID, err = a.d.Monitors.CreateMonitor(ctx, s, m, plan.MaxMonitors); err != nil {
		return domain.Monitor{}, err
	}
	a.d.Sched.Refresh()
	return a.d.Monitors.GetMonitor(ctx, s, m.ID)
}

// Update validates and replaces a monitor's definition.
func (a *Admin) Update(ctx context.Context, s domain.Scope, m domain.Monitor) (domain.Monitor, error) {
	plan, err := a.d.Plans.GetPlan(ctx, s)
	if err != nil {
		return domain.Monitor{}, err
	}
	if err := a.validate(m, plan); err != nil {
		return domain.Monitor{}, err
	}
	if err := a.d.Monitors.UpdateMonitor(ctx, s, m, a.d.Now().UTC()); err != nil {
		return domain.Monitor{}, err
	}
	a.d.Sched.Refresh()
	return a.d.Monitors.GetMonitor(ctx, s, m.ID)
}

func (a *Admin) validate(m domain.Monitor, plan domain.Plan) error {
	if err := domain.ValidateMonitor(m, plan.MinCheckInterval); err != nil {
		return err
	}
	if m.Kind == domain.MonitorPing && a.d.PingAvailable != nil {
		if err := a.d.PingAvailable(); err != nil {
			return fmt.Errorf("%w: ping monitors cannot run on this server: %w", domain.ErrInvalid, err)
		}
	}
	return nil
}

// Delete removes a monitor and its history.
func (a *Admin) Delete(ctx context.Context, s domain.Scope, id int64) error {
	if err := a.d.Monitors.DeleteMonitor(ctx, s, id); err != nil {
		return err
	}
	a.d.Sched.Refresh()
	return nil
}

// SetPaused pauses or resumes a monitor.
func (a *Admin) SetPaused(ctx context.Context, s domain.Scope, id int64, paused bool) error {
	if err := a.d.Monitors.SetPaused(ctx, s, id, paused, a.d.Now().UTC()); err != nil {
		return err
	}
	a.d.Sched.Refresh()
	return nil
}

// CheckNow asks for a check at once, at most every CheckNowEvery per monitor.
func (a *Admin) CheckNow(ctx context.Context, s domain.Scope, id int64) error {
	m, err := a.d.Monitors.GetMonitor(ctx, s, id) // tenant ownership first
	if err != nil {
		return err
	}
	if m.Paused {
		return fmt.Errorf("%w: the monitor is paused", domain.ErrConflict)
	}
	now := a.d.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	if last, ok := a.lastNow[id]; ok && now.Sub(last) < CheckNowEvery {
		return fmt.Errorf("%w: checked on demand less than %s ago", domain.ErrQuotaExceeded, CheckNowEvery)
	}
	if !a.d.Sched.CheckNow(id) {
		return fmt.Errorf("%w: a check is already running", domain.ErrConflict)
	}
	a.lastNow[id] = now
	return nil
}

// Results returns a monitor's checks in [from, to): at most 7 days, the
// newest MaxResults when there are more.
func (a *Admin) Results(ctx context.Context, s domain.Scope, id int64, from, to time.Time) ([]domain.CheckResult, error) {
	if !from.Before(to) || to.Sub(from) > 7*24*time.Hour {
		return nil, fmt.Errorf("%w: from must be before to, at most 7 days apart", domain.ErrInvalid)
	}
	if _, err := a.d.Monitors.GetMonitor(ctx, s, id); err != nil {
		return nil, err
	}
	return a.d.History.MonitorResults(ctx, s, id, from, to, MaxResults)
}

// MaxResults bounds one results read (a week at one check a minute is 10 080).
const MaxResults = 20_000

// Incidents returns a monitor's newest incidents and events.
func (a *Admin) Incidents(ctx context.Context, s domain.Scope, id int64, limit int) ([]domain.Incident, []domain.MonitorEvent, error) {
	if _, err := a.d.Monitors.GetMonitor(ctx, s, id); err != nil {
		return nil, nil, err
	}
	inc, err := a.d.History.MonitorIncidents(ctx, s, id, limit)
	if err != nil {
		return nil, nil, err
	}
	evs, err := a.d.States.ListMonitorEvents(ctx, s, id, limit)
	return inc, evs, err
}
