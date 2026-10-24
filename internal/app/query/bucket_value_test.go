package query

import (
	"testing"
	"time"
)

func TestBucketValue(t *testing.T) {
	t0 := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	b := RollupBucket{Start: t0, N: 4,
		Gauges:   map[Gauge]Agg{GaugeCPU: {Sum: 200, Min: 10, Max: 90}},
		Counters: map[string]Edge{CounterNetRx: {First: 1000, Last: 7000}},
		FirstTS:  t0, LastTS: t0.Add(30 * time.Second)}
	for _, c := range []struct {
		metric, agg string
		want        float64
	}{
		{"cpu", "avg", 50}, {"cpu", "max", 90}, {"cpu", "min", 10},
		{"net_rx_rate", "max", 200}, // growth over the 30 s span, not the 60 s bucket
	} {
		got, ok := BucketValue(b, c.metric, c.agg)
		if !ok || got != c.want {
			t.Errorf("%s/%s = %v, %v; want %v", c.metric, c.agg, got, ok, c.want)
		}
	}
	single := b
	single.LastTS = single.FirstTS
	if _, ok := BucketValue(single, "net_rx_rate", "avg"); ok {
		t.Error("a single-sample bucket produced a rate")
	}
	if _, ok := BucketValue(RollupBucket{}, "cpu", "avg"); ok {
		t.Error("an empty bucket produced a value")
	}
	if !IsKnownMetric("disk_w_rate") || IsKnownMetric("nope") || !IsRate("net_tx_rate") || IsRate("cpu") {
		t.Error("metric catalogue wrong")
	}
}
