package domain

import "fmt"

// DetailMetric describes a variable-cardinality metric the agent may send.
// The catalog is closed: an unknown metric name is refused rather than
// growing storage with whatever an agent invents.
type DetailMetric struct {
	Name  string
	Kind  MetricKind
	Label string // the one label key that distinguishes its series
	Unit  string // "percent" or "bytes"
	// Scale is the fixed-point factor storage uses: 100 for percentages
	// (two decimals), 1 for byte counts.
	Scale int64
}

// Detail metric names.
const (
	MetricCPUCore   = "cpu_core_percent"
	MetricDiskUsed  = "disk_used_percent"
	MetricDiskRead  = "disk_read_bytes"
	MetricDiskWrite = "disk_write_bytes"
	MetricNetRx     = "net_rx_bytes"
	MetricNetTx     = "net_tx_bytes"
)

var catalog = map[string]DetailMetric{
	MetricCPUCore:   {Name: MetricCPUCore, Kind: KindGauge, Label: "core", Unit: "percent", Scale: 100},
	MetricDiskUsed:  {Name: MetricDiskUsed, Kind: KindGauge, Label: "mount", Unit: "percent", Scale: 100},
	MetricDiskRead:  {Name: MetricDiskRead, Kind: KindCounter, Label: "device", Unit: "bytes", Scale: 1},
	MetricDiskWrite: {Name: MetricDiskWrite, Kind: KindCounter, Label: "device", Unit: "bytes", Scale: 1},
	MetricNetRx:     {Name: MetricNetRx, Kind: KindCounter, Label: "nic", Unit: "bytes", Scale: 1},
	MetricNetTx:     {Name: MetricNetTx, Kind: KindCounter, Label: "nic", Unit: "bytes", Scale: 1},
}

// LookupMetric returns a catalog entry.
func LookupMetric(name string) (DetailMetric, bool) {
	m, ok := catalog[name]
	return m, ok
}

// CheckDetail validates a detail sample against the catalog: known metric,
// matching kind, exactly its one label, and a value storage can hold.
func CheckDetail(d DetailSample) error {
	if err := d.Validate(); err != nil {
		return err
	}
	m, ok := catalog[d.Metric]
	if !ok {
		return fmt.Errorf("%w: unknown detail metric %q", ErrInvalid, d.Metric)
	}
	if d.Kind != m.Kind {
		return fmt.Errorf("%w: metric %s is a %s, not a %s", ErrInvalid, d.Metric, m.Kind, d.Kind)
	}
	if v, ok := d.Labels[m.Label]; len(d.Labels) != 1 || !ok || v == "" {
		return fmt.Errorf("%w: metric %s needs exactly the label %q", ErrInvalid, d.Metric, m.Label)
	}
	if err := d.Labels.Validate(); err != nil {
		return err
	}
	if m.Unit == "percent" && d.Value > 100 {
		return fmt.Errorf("%w: %s = %v exceeds 100", ErrInvalid, d.Metric, d.Value)
	}
	if d.Value*float64(m.Scale) > float64(MaxCounter) || d.Value < 0 {
		return fmt.Errorf("%w: %s value out of range", ErrInvalid, d.Metric)
	}
	return nil
}

// CounterKey names a detail counter's reset-folding state. It shares the
// host_counters table with the core counters, so detail counters get the
// same reboot handling in the same transaction.
func (k SeriesKey) CounterKey() string { return "d:" + k.Metric + ":" + k.LabelsHash }
