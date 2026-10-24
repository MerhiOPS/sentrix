package domain

import (
	"fmt"
	"math"
	"time"
)

// CoreSample is the fixed set of host-wide values collected every interval.
// It is stored as one wide row (PLAN §4): the key is written once per sample,
// not once per value.
//
// Gauges are instantaneous. Counters are raw, monotonic totals as read on the
// host; rates are derived server-side with reset detection, because the agent
// is untrusted and a reboot resets every counter.
type CoreSample struct {
	TS time.Time

	// Gauges.
	CPUPercent    float64
	MemoryPercent float64
	DiskPercent   float64 // fullest mounted filesystem
	Load1         float64 // 0 on Windows, which has no load average
	Load5         float64
	Load15        float64
	MemoryUsed    uint64 // bytes
	UptimeSeconds uint64

	// Counters.
	NetRxBytes     uint64
	NetTxBytes     uint64
	DiskReadBytes  uint64
	DiskWriteBytes uint64
}

// Validate rejects samples that would poison storage or counter state:
// non-finite or out-of-range gauges (OLD A-M4) and a missing timestamp.
func (s CoreSample) Validate() error {
	if s.TS.IsZero() {
		return fmt.Errorf("%w: sample has no timestamp", ErrInvalid)
	}
	gauges := []struct {
		name string
		v    float64
		max  float64
	}{
		{"cpu_percent", s.CPUPercent, 100},
		{"memory_percent", s.MemoryPercent, 100},
		{"disk_percent", s.DiskPercent, 100},
		{"load_1m", s.Load1, math.MaxFloat64},
		{"load_5m", s.Load5, math.MaxFloat64},
		{"load_15m", s.Load15, math.MaxFloat64},
	}
	for _, g := range gauges {
		if err := checkGauge(g.name, g.v, g.max); err != nil {
			return err
		}
	}
	for _, c := range []uint64{s.MemoryUsed, s.UptimeSeconds, s.NetRxBytes, s.NetTxBytes, s.DiskReadBytes, s.DiskWriteBytes} {
		if c > MaxCounter {
			return fmt.Errorf("%w: counter value %d exceeds %d", ErrInvalid, c, uint64(MaxCounter))
		}
	}
	return nil
}

// MaxCounter is the largest integer value storage can hold (signed 64-bit).
// No real counter reaches it; a value above it is corrupt input.
const MaxCounter = math.MaxInt64

func checkGauge(name string, v, max float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fmt.Errorf("%w: %s is not finite", ErrInvalid, name)
	}
	if v < 0 || v > max {
		return fmt.Errorf("%w: %s = %v out of range [0, %v]", ErrInvalid, name, v, max)
	}
	return nil
}

// MetricKind says how a series' values combine over time.
type MetricKind string

const (
	KindGauge   MetricKind = "gauge"   // aggregate with min/max/avg
	KindCounter MetricKind = "counter" // monotonic; aggregate as rate over covered span
)

// DetailSample is one value of a variable-cardinality series (per core, per
// mount, per NIC). The series is identified by metric name + labels.
type DetailSample struct {
	TS     time.Time
	Metric string
	Kind   MetricKind
	Labels Labels
	Value  float64
}

// Validate rejects unusable detail samples.
func (d DetailSample) Validate() error {
	if d.TS.IsZero() {
		return fmt.Errorf("%w: detail sample has no timestamp", ErrInvalid)
	}
	if d.Metric == "" {
		return fmt.Errorf("%w: detail sample has no metric name", ErrInvalid)
	}
	if d.Kind != KindGauge && d.Kind != KindCounter {
		return fmt.Errorf("%w: metric %s has unknown kind %q", ErrInvalid, d.Metric, d.Kind)
	}
	if math.IsNaN(d.Value) || math.IsInf(d.Value, 0) {
		return fmt.Errorf("%w: metric %s value is not finite", ErrInvalid, d.Metric)
	}
	if d.Kind == KindCounter && d.Value < 0 {
		return fmt.Errorf("%w: counter %s is negative", ErrInvalid, d.Metric)
	}
	return nil
}
