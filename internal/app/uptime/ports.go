// Package uptime holds the uptime-monitoring use cases: managing monitors,
// scheduling checks, turning results into state, incidents and
// notifications, and reading history.
package uptime

import (
	"context"
	"errors"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// MonitorRepo stores monitor definitions. A monitor naming another tenant's
// channel is domain.ErrNotFound: tenant A cannot notify through tenant B.
type MonitorRepo interface {
	ListMonitors(ctx context.Context, s domain.Scope) ([]domain.Monitor, error)
	GetMonitor(ctx context.Context, s domain.Scope, id int64) (domain.Monitor, error)
	// CreateMonitor stores a new monitor in state pending. It is refused
	// with domain.ErrQuotaExceeded when the tenant already has max
	// monitors, counted in the same transaction as the insert.
	CreateMonitor(ctx context.Context, s domain.Scope, m domain.Monitor, max int) (int64, error)
	// UpdateMonitor replaces the definition (Paused is ignored). A changed
	// kind or target resets the state to pending and ends any open
	// incident at at: the old state described another target.
	UpdateMonitor(ctx context.Context, s domain.Scope, m domain.Monitor, at time.Time) error
	DeleteMonitor(ctx context.Context, s domain.Scope, id int64) error
	// SetPaused pauses (state paused, any open incident ended at at) or
	// resumes (state pending) a monitor. Repeating the current setting is
	// a no-op.
	SetPaused(ctx context.Context, s domain.Scope, id int64, paused bool, at time.Time) error
}

// CheckRecord is everything one check changes.
type CheckRecord struct {
	Monitor domain.Monitor
	Result  domain.CheckResult
	Step    domain.MonitorStep // Events carry their Notify decision
	// Revision is the state revision the check started from.
	Revision int64
}

// ErrStale means the monitor changed while its check ran (paused, resumed,
// edited or deleted); the result is discarded.
var ErrStale = errors.New("monitor changed while it was being checked")

// StateRepo persists results and their consequences.
type StateRepo interface {
	MonitorStates(ctx context.Context, s domain.Scope) ([]domain.MonitorState, error)
	// RecordCheck stores, in one transaction (R1): the result, the hourly
	// rollup (covered and reclassified time, checks, latency), metering,
	// incident changes, the events (before the state, as for alerts) and
	// the next state. It returns the event ids in Step.Events order, or
	// ErrStale when the state's revision is no longer rec.Revision.
	RecordCheck(ctx context.Context, s domain.Scope, rec CheckRecord) ([]int64, error)
	SetMonitorDelivery(ctx context.Context, s domain.Scope, event int64, outcome, delivery string) error
	// ListMonitorEvents returns the newest events first; monitor 0 means
	// every monitor of the tenant.
	ListMonitorEvents(ctx context.Context, s domain.Scope, monitor int64, limit int) ([]domain.MonitorEvent, error)
}

// HistoryReader reads what checks left behind.
type HistoryReader interface {
	// MonitorResults returns results in [from, to), oldest first, at most
	// limit (the newest ones when there are more).
	MonitorResults(ctx context.Context, s domain.Scope, monitor int64, from, to time.Time, limit int) ([]domain.CheckResult, error)
	// MonitorHours returns hourly rollups with Hour in [from, to), oldest
	// first; monitor 0 means every monitor of the tenant.
	MonitorHours(ctx context.Context, s domain.Scope, monitor int64, from, to time.Time) ([]domain.MonitorHour, error)
	// MonitorIncidents returns the newest incidents first.
	MonitorIncidents(ctx context.Context, s domain.Scope, monitor int64, limit int) ([]domain.Incident, error)
}

// Scheduled is one runnable monitor with what scheduling needs.
type Scheduled struct {
	Tenant  domain.TenantID
	Monitor domain.Monitor
	State   domain.MonitorState
}

// ScheduleReader lists every unpaused monitor of every tenant: the one
// place uptime crosses tenants, so it takes an OpsScope.
type ScheduleReader interface {
	ScheduledMonitors(ctx context.Context, ops domain.OpsScope) ([]Scheduled, error)
}

// Sweeper settles monitor notifications a previous process queued but
// never delivered. Run at startup, before the dispatcher.
type Sweeper interface {
	FailUndeliveredMonitorEvents(ctx context.Context, ops domain.OpsScope, reason string) (int, error)
}
