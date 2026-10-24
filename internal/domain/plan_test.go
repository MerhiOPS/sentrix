package domain

import (
	"errors"
	"testing"
)

func TestPlanRetentionCapsMetricsAndSetsLogs(t *testing.T) {
	r := Plan{MaxHosts: 5, MetricsRetentionDays: 14, LogsRetentionDays: 3}.Retention(DefaultRetention)
	if r[DataRawCore] != 7*day || r[DataRollup1m] != 14*day || r[DataRollup1h] != 14*day || r[DataLogs] != 3*day {
		t.Fatalf("retention %v", r)
	}
	long := DefaultPlan.Retention(DefaultRetention)
	if long[DataRollup1h] != 400*day || long[DataLogs] != 14*day || long[DataProcesses] != 3*day {
		t.Fatalf("default plan retention %v", long)
	}
}

func TestValidatePlan(t *testing.T) {
	if err := ValidatePlan(DefaultPlan); err != nil {
		t.Fatal(err)
	}
	for _, p := range []Plan{{MaxHosts: 0, MetricsRetentionDays: 1, LogsRetentionDays: 1},
		{MaxHosts: 1, MetricsRetentionDays: 0, LogsRetentionDays: 1}, {MaxHosts: 1, MetricsRetentionDays: 1, LogsRetentionDays: 1, LogBytesPerDay: -1}} {
		if err := ValidatePlan(p); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v accepted", p)
		}
	}
}
