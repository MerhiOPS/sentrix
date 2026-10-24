package domain

import (
	"errors"
	"testing"
	"time"
)

func validRule() AlertRule {
	return AlertRule{Name: "cpu", Kind: RuleThreshold, Metric: "cpu", Aggregation: AggAvg, Comparator: CmpGT,
		Threshold: 90, Duration: 5 * time.Minute, OnNoData: NoDataHold, Enabled: true}
}

func TestValidateRule(t *testing.T) {
	known := func(m string) bool { return m == "cpu" }
	if err := ValidateRule(validRule(), known); err != nil {
		t.Fatalf("valid rule refused: %v", err)
	}
	for name, mut := range map[string]func(*AlertRule){
		"empty name":       func(r *AlertRule) { r.Name = " " },
		"sub-minute":       func(r *AlertRule) { r.Duration = 30 * time.Second },
		"fractional":       func(r *AlertRule) { r.Duration = 90 * time.Second },
		"too long":         func(r *AlertRule) { r.Duration = 25 * time.Hour },
		"negative cool":    func(r *AlertRule) { r.Cooldown = -time.Minute },
		"unknown kind":     func(r *AlertRule) { r.Kind = "rate" },
		"unknown metric":   func(r *AlertRule) { r.Metric = "gpu" },
		"bad aggregation":  func(r *AlertRule) { r.Aggregation = "p99" },
		"bad comparator":   func(r *AlertRule) { r.Comparator = "eq" },
		"missing no_data":  func(r *AlertRule) { r.OnNoData = "" },
		"too many hosts":   func(r *AlertRule) { r.Hosts = make([]HostID, MaxRuleHosts+1) },
		"too many targets": func(r *AlertRule) { r.Channels = make([]int64, MaxRuleChannels+1) },
	} {
		r := validRule()
		mut(&r)
		if err := ValidateRule(r, known); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: accepted (%v)", name, err)
		}
	}
	noData := AlertRule{Name: "silent", Kind: RuleNoData, Duration: 10 * time.Minute}
	if err := ValidateRule(noData, known); err != nil {
		t.Errorf("no_data rule needs no metric: %v", err)
	}
}

func TestHolds(t *testing.T) {
	r := validRule()
	if !r.Holds(91) || r.Holds(90) {
		t.Error("gt wrong at the threshold")
	}
	r.Comparator = CmpLT
	if !r.Holds(89) || r.Holds(90) {
		t.Error("lt wrong at the threshold")
	}
}

func TestSilenceCovers(t *testing.T) {
	t0 := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	rule, host, other := int64(1), NewHostID(), NewHostID()
	s := Silence{Rule: &rule, Host: &host, Starts: t0, Ends: t0.Add(time.Hour)}
	if !s.Covers(1, host, t0) || s.Covers(1, host, t0.Add(time.Hour)) || s.Covers(1, host, t0.Add(-time.Second)) {
		t.Error("window bounds wrong: want [Starts, Ends)")
	}
	if s.Covers(2, host, t0) || s.Covers(1, other, t0) {
		t.Error("covered another rule or host")
	}
	all := Silence{Starts: t0, Ends: t0.Add(time.Hour)}
	if !all.Covers(9, other, t0) {
		t.Error("an unscoped silence did not cover everything")
	}
	if ValidateSilence(Silence{Starts: t0, Ends: t0}) == nil || ValidateSilence(Silence{Starts: t0, Ends: t0.Add(31 * 24 * time.Hour)}) == nil {
		t.Error("an empty or over-long silence was accepted")
	}
}

func TestMaskedURL(t *testing.T) {
	c := Channel{URL: "https://hooks.slack.com/services/T/B/secret"}
	if got := c.MaskedURL(); got != "https://hooks.slack.com/"+MaskSentinel || !IsMasked(got) {
		t.Errorf("masked = %q", got)
	}
	if (Channel{URL: "::"}).MaskedURL() != MaskSentinel {
		t.Error("an unparsable URL was not fully masked")
	}
}
