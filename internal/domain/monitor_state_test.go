package domain

import (
	"testing"
	"time"
)

var m0 = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// run feeds results through StepMonitor and returns every step.
func run(m Monitor, st MonitorState, results ...CheckResult) ([]MonitorStep, MonitorState) {
	var steps []MonitorStep
	for _, r := range results {
		s := StepMonitor(m, st, r)
		steps = append(steps, s)
		st = s.Next
	}
	return steps, st
}

func up(at time.Duration) CheckResult { return CheckResult{At: m0.Add(at), Outcome: CheckUp} }
func down(at time.Duration, c ErrorClass) CheckResult {
	return CheckResult{At: m0.Add(at), Outcome: CheckDown, Class: c}
}

func kinds(steps []MonitorStep) []MonitorEventKind {
	var out []MonitorEventKind
	for _, s := range steps {
		for _, e := range s.Events {
			out = append(out, e.Kind)
		}
	}
	return out
}

func TestOneFailedCheckOpensNoIncident(t *testing.T) {
	m := httpMonitor() // one confirmation retry
	steps, st := run(m, MonitorState{Status: MonitorPending},
		up(0), down(time.Minute, ErrTimeout), up(time.Minute+10*time.Second), up(2*time.Minute))
	if len(kinds(steps)) != 0 || st.Status != MonitorUp || st.Failures != 0 {
		t.Fatalf("events %v, status %s, failures %d: a blip must not open an incident", kinds(steps), st.Status, st.Failures)
	}
	if !steps[1].Next.NeedsRetry() {
		t.Fatal("after one failure the monitor must ask for a confirmation re-check")
	}
	for i, s := range steps {
		if s.Open != nil || s.Reclassify != nil || s.CoveredDown {
			t.Fatalf("step %d: %+v", i, s)
		}
	}
}

func TestConfirmedFailureAndRecoveryCycle(t *testing.T) {
	m := httpMonitor()
	steps, st := run(m, MonitorState{Status: MonitorPending},
		up(0),                                // 0: pending → up
		down(time.Minute, ErrStatus),         // 1: first failure at 12:01
		down(time.Minute+10*time.Second, ""), // 2: confirmed at 12:01:10
		down(2*time.Minute, ErrTimeout),      // 3: still down
		up(3*time.Minute),                    // 4: recovered at 12:03
		up(4*time.Minute))                    // 5
	if got := kinds(steps); len(got) != 2 || got[0] != EventDown || got[1] != EventUp {
		t.Fatalf("events %v, want [down up]", got)
	}
	open := steps[2].Open
	if open == nil || !open.Start.Equal(m0.Add(time.Minute)) || open.Class != ErrNone || open.Failures != 2 {
		t.Fatalf("incident %+v: must start at the first failed check", open)
	}
	if steps[2].Events[0].Class != ErrNone || steps[1].Next.FirstFailure != m0.Add(time.Minute) {
		t.Fatalf("down event %+v", steps[2].Events[0])
	}
	if r := steps[2].Reclassify; r == nil || !r.Start.Equal(m0.Add(time.Minute)) || !r.End.Equal(m0.Add(70*time.Second)) {
		t.Fatalf("reclassify %+v: the confirmation window was counted as up and is downtime", r)
	}
	if !steps[3].Failure || steps[3].Open != nil || len(steps[3].Events) != 0 {
		t.Fatalf("step 3 %+v: a failure while down adds to the incident, nothing else", steps[3])
	}
	if steps[4].Close == nil || !steps[4].Close.Equal(m0.Add(3*time.Minute)) || steps[4].Events[0].DownFor != 2*time.Minute {
		t.Fatalf("recovery %+v: down for 2m from the first failure", steps[4])
	}
	if st.Status != MonitorUp || !st.Since.Equal(m0.Add(3*time.Minute)) {
		t.Fatalf("final state %+v", st)
	}
	// Attribution: everything after the confirmation, until recovery, is
	// down; plus the reclassified window, downtime equals the incident.
	var downtime time.Duration
	for _, s := range steps {
		if s.CoveredDown {
			downtime += s.Covered.End.Sub(s.Covered.Start)
		}
		if s.Reclassify != nil {
			downtime += s.Reclassify.End.Sub(s.Reclassify.Start)
		}
	}
	if downtime != 2*time.Minute {
		t.Fatalf("attributed downtime %s, want the incident's 2m", downtime)
	}
}

func TestNoRetriesConfirmsAtOnce(t *testing.T) {
	m := httpMonitor()
	m.ConfirmRetries = 0
	steps, _ := run(m, MonitorState{Status: MonitorPending}, down(0, ErrDNS))
	if got := kinds(steps); len(got) != 1 || got[0] != EventDown || steps[0].Reclassify != nil {
		t.Fatalf("events %v reclassify %v: a pending monitor down at once has no up time to reclassify", got, steps[0].Reclassify)
	}
}

func TestUnknownChangesNothing(t *testing.T) {
	m := httpMonitor()
	prev := MonitorState{Status: MonitorUp, Since: m0, LastCheck: m0, Failures: 1, FirstFailure: m0}
	s := StepMonitor(m, prev, CheckResult{At: m0.Add(time.Minute), Outcome: CheckUnknown, Class: ErrConnect})
	want := prev
	want.LastCheck = m0.Add(time.Minute)
	if s.Next != want || len(s.Events) != 0 || s.Covered != (Span{}) || s.Open != nil {
		t.Fatalf("unknown result changed state: %+v", s)
	}
}

func TestLongGapsAreNotVouchedFor(t *testing.T) {
	m := httpMonitor()
	prev := MonitorState{Status: MonitorUp, Since: m0, LastCheck: m0}
	if s := StepMonitor(m, prev, up(2*time.Minute)); s.Covered.End.Sub(s.Covered.Start) != 2*time.Minute {
		t.Fatalf("a gap of two intervals is covered: %+v", s.Covered)
	}
	if s := StepMonitor(m, prev, up(2*time.Minute+time.Second)); s.Covered != (Span{}) {
		t.Fatalf("a longer gap (server not checking) must not count as up: %+v", s.Covered)
	}
	pending := MonitorState{Status: MonitorPending, LastCheck: m0}
	if s := StepMonitor(m, pending, up(time.Minute)); s.Covered != (Span{}) {
		t.Fatalf("time while pending is not monitored time: %+v", s.Covered)
	}
}

func TestCertificateWarnings(t *testing.T) {
	m := httpMonitor() // warn 14 days ahead
	cert := func(at time.Duration, left time.Duration) CheckResult {
		r := up(at)
		exp := r.At.Add(left)
		r.CertExpiry = &exp
		return r
	}
	day := 24 * time.Hour
	steps, st := run(m, MonitorState{Status: MonitorPending},
		cert(0, 30*day),           // ok
		cert(time.Minute, 14*day), // expiring: warn once
		cert(2*time.Minute, 13*day),
		cert(3*time.Minute, 90*day)) // renewed
	if got := kinds(steps); len(got) != 2 || got[0] != EventCertExpiring || got[1] != EventCertRenewed {
		t.Fatalf("events %v, want [cert_expiring cert_renewed]", got)
	}
	if len(steps[1].Events) != 1 {
		t.Fatal("exactly cert_warn_days left must warn on that check")
	}
	if st.Cert != CertOK || st.CertExpiry == nil || !st.CertExpiry.Equal(m0.Add(3*time.Minute+90*day)) {
		t.Fatalf("state %+v", st)
	}
	m.CertWarnDays = 0
	if s := StepMonitor(m, MonitorState{Status: MonitorUp, Cert: CertExpiring}, cert(0, day)); len(s.Events) != 0 || s.Next.Cert != CertUnknown {
		t.Fatalf("warnings off: %+v", s)
	}
}
