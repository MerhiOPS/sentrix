package query

import (
	"context"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Resolution is a rollup bucket width.
type Resolution string

const (
	Res1m Resolution = "1m"
	Res1h Resolution = "1h"
)

// Width returns the bucket width.
func (r Resolution) Width() time.Duration {
	if r == Res1h {
		return time.Hour
	}
	return time.Minute
}

// Agg is a gauge's aggregate over a bucket.
type Agg struct{ Sum, Min, Max float64 }

// Edge is a counter's first and last value in a bucket.
type Edge struct{ First, Last float64 }

// Counter rollup names, matching the core counters.
const (
	CounterNetRx = "net_rx"
	CounterNetTx = "net_tx"
	CounterDiskR = "disk_r"
	CounterDiskW = "disk_w"
)

// RollupBucket is one bucket of core rollups. Counters share FirstTS/LastTS
// because all core counters are sampled at the same instant.
type RollupBucket struct {
	Start           time.Time
	N               int
	Gauges          map[Gauge]Agg
	Counters        map[string]Edge
	FirstTS, LastTS time.Time
}

// RollupReader reads core rollups in [from, to) by bucket start.
type RollupReader interface {
	Rollups(ctx context.Context, s domain.Scope, host domain.HostID, res Resolution, from, to time.Time) ([]RollupBucket, error)
}

// DetailPoint is one stored detail value (counters reset-adjusted).
type DetailPoint struct {
	TS    time.Time
	Value float64
}

// DetailReader reads detail series. A series id is only meaningful with
// its host: another host's (or tenant's) series id is domain.ErrNotFound.
type DetailReader interface {
	ListSeries(ctx context.Context, s domain.Scope, host domain.HostID) ([]domain.Series, error)
	DetailPoints(ctx context.Context, s domain.Scope, host domain.HostID, series int64, from, to time.Time) ([]DetailPoint, error)
}

// ProcessReader reads process snapshots.
type ProcessReader interface {
	// ProcessesAt returns the newest snapshot at or before at, or
	// domain.ErrNotFound when there is none.
	ProcessesAt(ctx context.Context, s domain.Scope, host domain.HostID, at time.Time) (domain.ProcessSnapshot, error)
}
