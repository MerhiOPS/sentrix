package domain

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestCheckDetail(t *testing.T) {
	ts := time.Now()
	ok := DetailSample{TS: ts, Metric: MetricCPUCore, Kind: KindGauge, Labels: Labels{"core": "3"}, Value: 42.5}
	if err := CheckDetail(ok); err != nil {
		t.Fatal(err)
	}
	for name, d := range map[string]DetailSample{
		"unknown metric": {TS: ts, Metric: "gpu_temp", Kind: KindGauge, Labels: Labels{"gpu": "0"}, Value: 1},
		"wrong kind":     {TS: ts, Metric: MetricNetRx, Kind: KindGauge, Labels: Labels{"nic": "eth0"}, Value: 1},
		"wrong label":    {TS: ts, Metric: MetricCPUCore, Kind: KindGauge, Labels: Labels{"cpu": "3"}, Value: 1},
		"extra label":    {TS: ts, Metric: MetricCPUCore, Kind: KindGauge, Labels: Labels{"core": "3", "x": "y"}, Value: 1},
		"empty label":    {TS: ts, Metric: MetricCPUCore, Kind: KindGauge, Labels: Labels{"core": ""}, Value: 1},
		"percent > 100":  {TS: ts, Metric: MetricDiskUsed, Kind: KindGauge, Labels: Labels{"mount": "/"}, Value: 101},
		"too large":      {TS: ts, Metric: MetricNetRx, Kind: KindCounter, Labels: Labels{"nic": "eth0"}, Value: math.MaxFloat64},
	} {
		if err := CheckDetail(d); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestCounterKeyDistinguishesSeries(t *testing.T) {
	a := KeyFor(MetricNetRx, Labels{"nic": "eth0"}).CounterKey()
	b := KeyFor(MetricNetRx, Labels{"nic": "eth1"}).CounterKey()
	c := KeyFor(MetricNetTx, Labels{"nic": "eth0"}).CounterKey()
	if a == b || a == c || !strings.HasPrefix(a, "d:") {
		t.Fatalf("counter keys collide or lack the detail prefix: %s %s %s", a, b, c)
	}
}

func TestProcessSnapshotValidate(t *testing.T) {
	ok := ProcessSnapshot{TS: time.Now(), Procs: []Process{{Name: "postgres", CPUPercent: 250, RSS: 1 << 30}}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("multi-core CPU > 100 must be allowed: %v", err)
	}
	many := ProcessSnapshot{TS: time.Now()}
	for i := 0; i <= MaxSnapshotProcs; i++ {
		many.Procs = append(many.Procs, Process{Name: "p"})
	}
	for name, s := range map[string]ProcessSnapshot{
		"no ts":     {Procs: ok.Procs},
		"too many":  many,
		"no name":   {TS: time.Now(), Procs: []Process{{CPUPercent: 1}}},
		"NaN cpu":   {TS: time.Now(), Procs: []Process{{Name: "x", CPUPercent: math.NaN()}}},
		"long name": {TS: time.Now(), Procs: []Process{{Name: strings.Repeat("x", MaxProcessNameLen+1)}}},
	} {
		if err := s.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}
