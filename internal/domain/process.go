package domain

import (
	"fmt"
	"math"
	"time"
)

// Process is one entry of a top-N snapshot. Only the name is kept: command
// lines carry secrets (tokens, passwords in arguments) and are never
// collected (PLAN §1).
type Process struct {
	Name       string
	CPUPercent float64 // of one core; can exceed 100 on multi-core hosts
	RSS        uint64  // bytes
}

// ProcessSnapshot is the top processes by CPU and by RSS at one instant.
// It is a snapshot, not a time series: PIDs recycle, and per-process series
// would grow cardinality forever (OLD DESIGN §4.2).
type ProcessSnapshot struct {
	TS    time.Time
	Procs []Process
}

// Snapshot limits.
const (
	MaxSnapshotProcs   = 40 // union of top 20 by CPU and top 20 by RSS
	MaxProcessNameLen  = 128
	ProcessSnapshotGap = 30 * time.Second
)

// Validate rejects snapshots storage must not accept.
func (p ProcessSnapshot) Validate() error {
	if p.TS.IsZero() {
		return fmt.Errorf("%w: process snapshot has no timestamp", ErrInvalid)
	}
	if len(p.Procs) > MaxSnapshotProcs {
		return fmt.Errorf("%w: %d processes, max %d", ErrInvalid, len(p.Procs), MaxSnapshotProcs)
	}
	for _, pr := range p.Procs {
		if pr.Name == "" || len(pr.Name) > MaxProcessNameLen {
			return fmt.Errorf("%w: process name length", ErrInvalid)
		}
		if math.IsNaN(pr.CPUPercent) || math.IsInf(pr.CPUPercent, 0) || pr.CPUPercent < 0 {
			return fmt.Errorf("%w: process %s cpu is not a finite, non-negative number", ErrInvalid, pr.Name)
		}
		if pr.RSS > MaxCounter {
			return fmt.Errorf("%w: process %s rss out of range", ErrInvalid, pr.Name)
		}
	}
	return nil
}
