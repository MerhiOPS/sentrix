package domain

import (
	"fmt"
	"time"
)

// Plan is what a tenant may use. Limits are enforced where data enters:
// max_hosts at enrollment (existing hosts keep reporting), the log budget in
// the batch transaction, retention by the janitor.
type Plan struct {
	MaxHosts             int
	LogBytesPerDay       int64
	MetricsRetentionDays int
	LogsRetentionDays    int
}

// DefaultPlan is what a tenant gets at creation.
var DefaultPlan = Plan{MaxHosts: 100, LogBytesPerDay: 1 << 30, MetricsRetentionDays: 400, LogsRetentionDays: 14}

// ValidatePlan checks a plan's ranges.
func ValidatePlan(p Plan) error {
	switch {
	case p.MaxHosts < 1 || p.MaxHosts > 100_000:
		return fmt.Errorf("%w: max_hosts must be 1-100000", ErrInvalid)
	case p.LogBytesPerDay < 0 || p.LogBytesPerDay > 1<<40:
		return fmt.Errorf("%w: log_bytes_per_day must be 0-1 TiB", ErrInvalid)
	case p.MetricsRetentionDays < 1 || p.MetricsRetentionDays > 3650:
		return fmt.Errorf("%w: metrics_retention_days must be 1-3650", ErrInvalid)
	case p.LogsRetentionDays < 1 || p.LogsRetentionDays > 3650:
		return fmt.Errorf("%w: logs_retention_days must be 1-3650", ErrInvalid)
	}
	return nil
}

// Retention applies the plan to the defaults: metrics_retention_days caps
// every metric kind (so it is how long any metric history exists — the
// hourly rollups, by default), logs_retention_days sets logs.
func (p Plan) Retention(defaults Retention) Retention {
	out := Retention{}
	ceiling := time.Duration(p.MetricsRetentionDays) * day
	for k, d := range defaults {
		out[k] = min(d, ceiling)
	}
	out[DataLogs] = time.Duration(p.LogsRetentionDays) * day
	return out
}

// Usage is a tenant's consumption, metered on the write path.
type Usage struct {
	Hosts         int   // enabled hosts now (against MaxHosts)
	LogBytesToday int64 // against LogBytesPerDay
	// This calendar month (UTC):
	MonthSamples   int64
	MonthLogBytes  int64
	MonthPeakHosts int // most distinct hosts reporting on one day
}
