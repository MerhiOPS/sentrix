// Package alerting evaluates alert rules against 1-minute rollups, keeps
// per-(rule, host) state, and records events before notifying.
package alerting

import (
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Observation is one evaluation's input for one (rule, host).
type Observation struct {
	// Bucket is the 1-minute window evaluated: [Start, End).
	Start, End time.Time
	HasValue   bool
	Value      float64
	// SilentFor is how long the host has sent nothing (no_data rules).
	SilentFor time.Duration
}

// Step is a transition decision. Event is nil when nothing is recorded.
type Step struct {
	Next  domain.RuleHostState
	Event *domain.AlertEvent
}

// step is the state machine. It is pure: the caller supplies the previous
// state, the observation and the time, so every transition is testable on a
// fake clock.
//
// Threshold rules: a true bucket moves ok → pending (Since = bucket start);
// pending → firing once the condition has held for Duration of bucket time;
// a false bucket returns pending → ok silently and firing → ok with a
// "resolved" event. A bucket without data follows the rule's on_no_data.
// No-data rules: silence of at least Duration fires; any data resolves.
func step(rule domain.AlertRule, prev domain.RuleHostState, obs Observation, at time.Time) Step {
	cond, decided := condition(rule, obs)
	if !decided {
		return Step{Next: prev} // on_no_data: hold
	}
	next := prev
	switch {
	case prev.State == domain.StateFiring && !cond:
		next.State, next.Since = domain.StateOK, obs.End
		return Step{Next: next, Event: event(rule, prev, next, obs, at)}
	case prev.State == domain.StateFiring:
		return Step{Next: prev}
	case !cond:
		next.State, next.Since = domain.StateOK, obs.End
		return Step{Next: next} // pending → ok, or ok stays ok: not an event
	}
	if prev.State != domain.StatePending {
		next.State, next.Since = domain.StatePending, obs.Start
		if rule.Kind == domain.RuleNoData {
			next.Since = obs.End.Add(-obs.SilentFor)
		}
	}
	if obs.End.Sub(next.Since) < rule.Duration {
		return Step{Next: next}
	}
	next.State = domain.StateFiring
	return Step{Next: next, Event: event(rule, prev, next, obs, at)}
}

// condition reports the rule's condition for the observation, and whether
// it is decided at all (false only for on_no_data = hold).
func condition(rule domain.AlertRule, obs Observation) (bool, bool) {
	if rule.Kind == domain.RuleNoData {
		return obs.SilentFor >= rule.Duration, true
	}
	if obs.HasValue {
		return rule.Holds(obs.Value), true
	}
	switch rule.OnNoData {
	case domain.NoDataFire:
		return true, true
	case domain.NoDataResolve:
		return false, true
	default:
		return false, false
	}
}

func event(rule domain.AlertRule, prev, next domain.RuleHostState, obs Observation, at time.Time) *domain.AlertEvent {
	ev := &domain.AlertEvent{Rule: rule.ID, Host: next.Host, From: prev.State, To: next.State, At: at}
	if obs.HasValue {
		v := obs.Value
		ev.Value = &v
	}
	return ev
}

// shouldNotify decides an event's notification: never while silenced; a
// repeat firing inside the cooldown is suppressed, a resolve never is (OLD:
// a cooldown that swallowed resolves left alerts open in the chat).
func shouldNotify(rule domain.AlertRule, st domain.RuleHostState, ev domain.AlertEvent, silenced bool) bool {
	if silenced {
		return false
	}
	if ev.To != domain.StateFiring {
		return true
	}
	return st.LastNotified == nil || ev.At.Sub(*st.LastNotified) >= rule.Cooldown
}
