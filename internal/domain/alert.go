package domain

import (
	"fmt"
	"strings"
	"time"
)

// RuleKind is what a rule watches.
type RuleKind string

const (
	RuleThreshold RuleKind = "threshold" // a metric crossing a value
	RuleNoData    RuleKind = "no_data"   // a host going silent
)

// Aggregation reduces one 1-minute bucket to a value.
type Aggregation string

const (
	AggAvg Aggregation = "avg"
	AggMax Aggregation = "max"
	AggMin Aggregation = "min"
)

// Comparator compares the value with the threshold.
type Comparator string

const (
	CmpGT Comparator = "gt"
	CmpLT Comparator = "lt"
)

// NoDataPolicy is what a threshold rule does for a host with no data.
// Every default is wrong somewhere, so each rule states its own.
type NoDataPolicy string

const (
	NoDataFire    NoDataPolicy = "fire"
	NoDataResolve NoDataPolicy = "resolve"
	NoDataHold    NoDataPolicy = "hold"
)

// AlertRule is a tenant's alert definition.
type AlertRule struct {
	ID          int64
	Name        string
	Kind        RuleKind
	Metric      string // a chartable core metric (cpu, mem, net_rx_rate, …); unused for no_data
	Aggregation Aggregation
	Comparator  Comparator
	Threshold   float64
	Duration    time.Duration // how long the condition must hold (no_data: silence before firing)
	Hosts       []HostID      // empty: every host of the tenant
	OnNoData    NoDataPolicy
	Cooldown    time.Duration // minimum gap between repeat firing notifications
	Channels    []int64
	Enabled     bool
}

// Rule limits.
const (
	MinRuleDuration = time.Minute // the 1m rollup is the evaluation grain
	MaxRuleDuration = 24 * time.Hour
	MaxRuleNameLen  = 100
	MaxRuleHosts    = 500
	MaxRuleChannels = 10
)

// ValidateRule checks a rule's shape. Which metrics exist is the query
// layer's knowledge, so the caller passes knownMetric.
func ValidateRule(r AlertRule, knownMetric func(string) bool) error {
	bad := func(f string, a ...any) error { return fmt.Errorf("%w: "+f, append([]any{ErrInvalid}, a...)...) }
	if n := strings.TrimSpace(r.Name); n == "" || len(n) > MaxRuleNameLen {
		return bad("name must be 1-%d characters", MaxRuleNameLen)
	}
	if r.Duration < MinRuleDuration || r.Duration > MaxRuleDuration || r.Duration%time.Minute != 0 {
		return bad("duration must be whole minutes between 1m and 24h")
	}
	if r.Cooldown < 0 || r.Cooldown > MaxRuleDuration {
		return bad("cooldown out of range")
	}
	if len(r.Hosts) > MaxRuleHosts || len(r.Channels) > MaxRuleChannels {
		return bad("too many hosts or channels")
	}
	switch r.Kind {
	case RuleNoData:
		return nil
	case RuleThreshold:
	default:
		return bad("kind must be threshold or no_data")
	}
	if !knownMetric(r.Metric) {
		return bad("unknown metric %q", r.Metric)
	}
	if r.Aggregation != AggAvg && r.Aggregation != AggMax && r.Aggregation != AggMin {
		return bad("aggregation must be avg, max or min")
	}
	if r.Comparator != CmpGT && r.Comparator != CmpLT {
		return bad("comparator must be gt or lt")
	}
	if r.OnNoData != NoDataFire && r.OnNoData != NoDataResolve && r.OnNoData != NoDataHold {
		return bad("on_no_data must be fire, resolve or hold")
	}
	return nil
}

// Holds reports whether value satisfies the rule's condition.
func (r AlertRule) Holds(value float64) bool {
	if r.Comparator == CmpLT {
		return value < r.Threshold
	}
	return value > r.Threshold
}

// AlertState is where one (rule, host) pair is.
type AlertState string

const (
	StateOK      AlertState = "ok"
	StatePending AlertState = "pending" // condition true, not yet for the full duration
	StateFiring  AlertState = "firing"
)

// RuleHostState is the persisted state of one (rule, host).
type RuleHostState struct {
	Rule         int64
	Host         HostID
	State        AlertState
	Since        time.Time  // when State was entered
	LastNotified *time.Time // last firing notification, for cooldown
}

// AlertEvent is one recorded transition.
type AlertEvent struct {
	ID       int64
	Rule     int64
	Host     HostID
	From     AlertState
	To       AlertState
	At       time.Time
	Value    *float64 // nil for no-data transitions
	Notify   bool     // whether a notification is due (not silenced, not in cooldown)
	Outcome  string   // "", "delivered", "failed"
	Delivery string   // the failure, joined across channels
}
