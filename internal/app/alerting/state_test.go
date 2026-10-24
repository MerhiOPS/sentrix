package alerting

import (
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

var t0 = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func cpuRule(d time.Duration, onNoData domain.NoDataPolicy) domain.AlertRule {
	return domain.AlertRule{ID: 1, Name: "cpu high", Kind: domain.RuleThreshold, Metric: "cpu", Aggregation: domain.AggAvg,
		Comparator: domain.CmpGT, Threshold: 90, Duration: d, OnNoData: onNoData, Enabled: true}
}

// bucket i is [t0+i m, t0+(i+1) m).
func obs(i int, value *float64) Observation {
	o := Observation{Start: t0.Add(time.Duration(i) * time.Minute), End: t0.Add(time.Duration(i+1) * time.Minute)}
	if value != nil {
		o.HasValue, o.Value = true, *value
	}
	return o
}

func v(x float64) *float64 { return &x }

// run feeds buckets through the machine and returns the state after each and
// the transitions recorded.
func run(rule domain.AlertRule, values []*float64) ([]domain.AlertState, []string) {
	st := domain.RuleHostState{Rule: rule.ID, State: domain.StateOK}
	var states []domain.AlertState
	var events []string
	for i, val := range values {
		s := step(rule, st, obs(i, val), t0.Add(time.Duration(i+1)*time.Minute+30*time.Second))
		st = s.Next
		states = append(states, st.State)
		if s.Event != nil {
			events = append(events, string(s.Event.From)+"→"+string(s.Event.To))
		}
	}
	return states, events
}

func TestFireAndResolveCycle(t *testing.T) {
	states, events := run(cpuRule(3*time.Minute, domain.NoDataHold), []*float64{v(50), v(95), v(96), v(97), v(98), v(40)})
	want := []domain.AlertState{"ok", "pending", "pending", "firing", "firing", "ok"}
	for i := range want {
		if states[i] != want[i] {
			t.Fatalf("states %v, want %v", states, want)
		}
	}
	if len(events) != 2 || events[0] != "pending→firing" || events[1] != "firing→ok" {
		t.Fatalf("events %v, want one firing and one resolve", events)
	}
}

func TestOneMinuteRuleFiresOnFirstTrueBucket(t *testing.T) {
	_, events := run(cpuRule(time.Minute, domain.NoDataHold), []*float64{v(99)})
	if len(events) != 1 {
		t.Fatalf("1m rule: events %v, want it to fire on the first true bucket", events)
	}
}

func TestSingleSpikeDoesNotFireFiveMinuteRule(t *testing.T) {
	states, events := run(cpuRule(5*time.Minute, domain.NoDataHold), []*float64{v(10), v(99), v(10), v(99), v(99), v(10)})
	if len(events) != 0 {
		t.Fatalf("spikes fired a 5m rule: states %v events %v", states, events)
	}
}

func TestOnNoDataResolveVsHold(t *testing.T) {
	firing := []*float64{v(99), v(99), nil, nil}
	_, held := run(cpuRule(2*time.Minute, domain.NoDataHold), firing)
	if len(held) != 1 {
		t.Fatalf("hold: events %v, want only the firing (no data keeps the state)", held)
	}
	_, resolved := run(cpuRule(2*time.Minute, domain.NoDataResolve), firing)
	if len(resolved) != 2 || resolved[1] != "firing→ok" {
		t.Fatalf("resolve: events %v, want firing then resolve on missing data", resolved)
	}
	_, fired := run(cpuRule(2*time.Minute, domain.NoDataFire), []*float64{nil, nil})
	if len(fired) != 1 {
		t.Fatalf("fire: events %v, want missing data to fire", fired)
	}
}

func TestNoDataRuleFiresOnSilence(t *testing.T) {
	rule := domain.AlertRule{ID: 2, Kind: domain.RuleNoData, Duration: 3 * time.Minute, Enabled: true}
	st := domain.RuleHostState{State: domain.StateOK}
	for i, silent := range []time.Duration{time.Minute, 2 * time.Minute, 3 * time.Minute} {
		o := obs(i, nil)
		o.SilentFor = silent
		s := step(rule, st, o, o.End)
		st = s.Next
		if (s.Event != nil) != (i == 2) {
			t.Fatalf("silent %v: event %v", silent, s.Event)
		}
	}
	o := obs(3, v(1))
	if s := step(rule, st, o, o.End); s.Event == nil || s.Next.State != domain.StateOK {
		t.Fatal("data again did not resolve the no_data alert")
	}
}

func TestCooldownSuppressesRepeatFiringNotResolve(t *testing.T) {
	rule := cpuRule(time.Minute, domain.NoDataHold)
	rule.Cooldown = 10 * time.Minute
	notified := t0
	st := domain.RuleHostState{State: domain.StateOK, LastNotified: &notified}
	fire := domain.AlertEvent{To: domain.StateFiring, At: t0.Add(5 * time.Minute)}
	if shouldNotify(rule, st, fire, false) {
		t.Fatal("repeat firing inside the cooldown was notified")
	}
	if !shouldNotify(rule, st, domain.AlertEvent{To: domain.StateOK, At: t0.Add(5 * time.Minute)}, false) {
		t.Fatal("a resolve inside the cooldown was suppressed")
	}
	if shouldNotify(rule, st, domain.AlertEvent{To: domain.StateOK, At: t0}, true) {
		t.Fatal("a silenced event was notified")
	}
	fire.At = t0.Add(11 * time.Minute)
	if !shouldNotify(rule, st, fire, false) {
		t.Fatal("firing after the cooldown was suppressed")
	}
}
