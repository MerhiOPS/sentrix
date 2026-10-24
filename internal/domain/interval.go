package domain

import (
	"fmt"
	"time"
)

// Sampling interval bounds (PLAN: 5 s default, 1–60 s configurable). An
// interval must be whole seconds so timestamps snap exactly to its grid.
const (
	MinInterval = time.Second
	MaxInterval = time.Minute
)

// ValidateInterval checks a sampling interval.
func ValidateInterval(d time.Duration) error {
	if d < MinInterval || d > MaxInterval || d%time.Second != 0 {
		return fmt.Errorf("%w: interval %v must be whole seconds in [%v, %v]",
			ErrInvalid, d, MinInterval, MaxInterval)
	}
	return nil
}

// SnapToGrid rounds t to the nearest multiple of interval since the Unix
// epoch. Exactly uniform timestamps are what make the timestamp column cheap
// and make "one sample per interval" a key constraint rather than a hope.
func SnapToGrid(t time.Time, interval time.Duration) time.Time {
	if interval <= 0 {
		return t
	}
	step := interval.Milliseconds()
	ms := t.UnixMilli()
	snapped := (ms + step/2) / step * step
	if ms < 0 {
		snapped = (ms - step/2) / step * step
	}
	return time.UnixMilli(snapped).UTC()
}
