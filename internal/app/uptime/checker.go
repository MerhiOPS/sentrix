package uptime

import (
	"context"
	"fmt"

	"github.com/MerhiOPS/sentrix/internal/app/alerting"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Prober runs one check of a monitor within its timeout.
type Prober interface {
	Probe(ctx context.Context, m domain.Monitor) domain.CheckResult
}

// Enqueuer accepts notification jobs (the alerting dispatcher).
type Enqueuer interface {
	Enqueue(j alerting.Job)
}

// CheckerDeps are the checker's ports.
type CheckerDeps struct {
	Probe    Prober
	States   StateRepo
	Channels alerting.ChannelRepo
	Silences alerting.SilenceRepo
	Out      Enqueuer
	// Refs, when set, decides whether a network-level failure is the
	// target's or the server's own.
	Refs *References
}

// Checker turns one check into a stored result and its notifications.
type Checker struct{ d CheckerDeps }

// NewChecker builds a checker.
func NewChecker(d CheckerDeps) *Checker { return &Checker{d: d} }

// networkClasses are failures the server's own outage would also cause.
var networkClasses = map[domain.ErrorClass]bool{
	domain.ErrDNS: true, domain.ErrConnect: true, domain.ErrTimeout: true, domain.ErrPingLoss: true,
}

// Check probes s's monitor, stores the result and everything it changes,
// and queues notifications for events that are due. It returns the state
// the next check starts from, or ErrStale if the monitor changed meanwhile.
func (c *Checker) Check(ctx context.Context, s Scheduled) (domain.MonitorState, error) {
	sc, err := domain.NewScope(s.Tenant)
	if err != nil {
		return s.State, err
	}
	r := c.d.Probe.Probe(ctx, s.Monitor)
	if r.Outcome == domain.CheckDown && networkClasses[r.Class] && c.d.Refs.Configured() && !c.d.Refs.Reachable(ctx) {
		r.Outcome = domain.CheckUnknown // the server is offline, not the target
	}
	step := domain.StepMonitor(s.Monitor, s.State, r)
	chans, err := c.decide(ctx, sc, s.Monitor, step.Events)
	if err != nil {
		return s.State, err
	}
	ids, err := c.d.States.RecordCheck(ctx, sc, CheckRecord{Monitor: s.Monitor, Result: r, Step: step, Revision: s.State.Revision})
	if err != nil {
		return s.State, err
	}
	for i, ev := range step.Events {
		if ev.Notify {
			c.d.Out.Enqueue(alerting.Job{Scope: sc, Event: ids[i], Channels: chans, Message: message(s.Monitor, ev),
				Recorder: deliveryRecorder{c.d.States}})
		}
	}
	next := step.Next
	next.Revision = s.State.Revision + 1
	return next, nil
}

// decide sets each event's Notify: it has channels and no silence covers
// the monitor. It returns the monitor's channels.
func (c *Checker) decide(ctx context.Context, sc domain.Scope, m domain.Monitor, evs []domain.MonitorEvent) ([]domain.Channel, error) {
	if len(evs) == 0 || len(m.Channels) == 0 {
		return nil, nil
	}
	all, err := c.d.Channels.ListChannels(ctx, sc)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	want := map[int64]bool{}
	for _, id := range m.Channels {
		want[id] = true
	}
	var chans []domain.Channel
	for _, ch := range all {
		if want[ch.ID] {
			chans = append(chans, ch)
		}
	}
	sils, err := c.d.Silences.ListSilences(ctx, sc)
	if err != nil {
		return nil, fmt.Errorf("list silences: %w", err)
	}
	for i := range evs {
		silenced := false
		for _, sil := range sils {
			silenced = silenced || sil.CoversMonitor(m.ID, evs[i].At)
		}
		evs[i].Notify = len(chans) > 0 && !silenced
	}
	return chans, nil
}

func message(m domain.Monitor, ev domain.MonitorEvent) alerting.Message {
	return alerting.Message{At: ev.At, Resolved: ev.Kind == domain.EventUp || ev.Kind == domain.EventCertRenewed,
		Monitor: &alerting.MonitorMessage{Name: m.Name, Target: m.Target, Event: ev.Kind, Class: ev.Class,
			DownFor: ev.DownFor, CertExpiry: ev.CertExpiry}}
}

// deliveryRecorder stores a monitor event's outcome on the monitor event.
type deliveryRecorder struct{ states StateRepo }

func (d deliveryRecorder) SetDelivery(ctx context.Context, s domain.Scope, event int64, outcome, delivery string) error {
	return d.states.SetMonitorDelivery(ctx, s, event, outcome, delivery)
}

// UndeliveredReason is recorded on monitor events swept at startup.
const UndeliveredReason = alerting.UndeliveredReason
