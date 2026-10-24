package alerting

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// EvalOffset is how far past the minute an evaluation runs: the bucket that
// just closed has had this long to receive its last batches. Together with
// the 1m grain it is the alert latency floor the UI states.
const EvalOffset = 30 * time.Second

// Deps are the evaluator's ports.
type Deps struct {
	Tenants  TenantLister
	Hosts    query.HostReader
	Rules    RuleRepo
	Channels ChannelRepo
	Silences SilenceRepo
	States   StateRepo
	Buckets  BucketReader
}

// Evaluator runs rules against the last complete 1m bucket, off the ingest
// path. It is driven by Tick, so tests run it on a fake clock.
type Evaluator struct {
	d    Deps
	out  *Dispatcher
	now  func() time.Time
	last time.Time // start of the last bucket evaluated
}

// NewEvaluator builds an evaluator delivering through out.
func NewEvaluator(d Deps, out *Dispatcher, now func() time.Time) *Evaluator {
	return &Evaluator{d: d, out: out, now: now}
}

// Tick evaluates the newest closed bucket once. A tick inside EvalOffset of
// the minute evaluates the bucket before it; a repeated tick is a no-op. One
// tenant's failure does not stop the others.
func (e *Evaluator) Tick(ctx context.Context) error {
	start := e.now().Add(-EvalOffset).Truncate(time.Minute).Add(-time.Minute)
	if !start.After(e.last) {
		return nil
	}
	ops, err := domain.NewOpsScope("alert evaluation")
	if err != nil {
		return err
	}
	tenants, err := e.d.Tenants.ListTenants(ctx, ops)
	if err != nil {
		return fmt.Errorf("list tenants: %w", err)
	}
	var errs []error
	for _, t := range tenants {
		s, err := domain.NewScope(t)
		if err == nil {
			err = e.evaluateTenant(ctx, s, start)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("tenant %v: %w", t, err))
		}
	}
	e.last = start
	return errors.Join(errs...)
}

// tenantView is everything one tenant's evaluation reads, loaded up front.
type tenantView struct {
	hosts    []domain.Host
	buckets  map[domain.HostID]query.RollupBucket
	states   map[stateKey]domain.RuleHostState
	silences []domain.Silence
	channels map[int64]domain.Channel
}

type stateKey struct {
	rule int64
	host domain.HostID
}

func (e *Evaluator) evaluateTenant(ctx context.Context, s domain.Scope, start time.Time) error {
	rules, err := e.d.Rules.ListRules(ctx, s)
	if err != nil {
		return err
	}
	rules = enabled(rules)
	if len(rules) == 0 {
		return nil
	}
	v, err := e.load(ctx, s, start)
	if err != nil {
		return err
	}
	obsAt := Observation{Start: start, End: start.Add(time.Minute)}
	at := e.now()
	for _, r := range rules {
		for _, h := range targets(r, v.hosts) {
			if err := e.evaluate(ctx, s, r, h, observe(r, h, v.buckets, obsAt), at, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Evaluator) load(ctx context.Context, s domain.Scope, start time.Time) (tenantView, error) {
	var v tenantView
	var err error
	if v.hosts, err = e.d.Hosts.ListHosts(ctx, s); err != nil {
		return v, err
	}
	if v.buckets, err = e.d.Buckets.TenantBuckets(ctx, s, start); err != nil {
		return v, err
	}
	states, err := e.d.States.AlertStates(ctx, s)
	if err != nil {
		return v, err
	}
	v.states = map[stateKey]domain.RuleHostState{}
	for _, st := range states {
		v.states[stateKey{st.Rule, st.Host}] = st
	}
	if v.silences, err = e.d.Silences.ListSilences(ctx, s); err != nil {
		return v, err
	}
	chans, err := e.d.Channels.ListChannels(ctx, s)
	v.channels = map[int64]domain.Channel{}
	for _, c := range chans {
		v.channels[c.ID] = c
	}
	return v, err
}

func (e *Evaluator) evaluate(ctx context.Context, s domain.Scope, r domain.AlertRule, h domain.Host, o Observation, at time.Time, v tenantView) error {
	prev, ok := v.states[stateKey{r.ID, h.ID}]
	if !ok {
		prev = domain.RuleHostState{Rule: r.ID, Host: h.ID, State: domain.StateOK}
	}
	st := step(r, prev, o, at)
	if st.Event == nil && st.Next.State == prev.State && st.Next.Since.Equal(prev.Since) {
		return nil
	}
	var chans []domain.Channel
	if ev := st.Event; ev != nil {
		chans = channelsOf(r, v.channels)
		ev.Notify = len(chans) > 0 && shouldNotify(r, prev, *ev, silenced(v.silences, r.ID, h.ID, at))
		if ev.Notify && ev.To == domain.StateFiring {
			st.Next.LastNotified = &at
		}
	}
	id, err := e.d.States.RecordTransition(ctx, s, st.Next, st.Event)
	if err != nil {
		return fmt.Errorf("record transition: %w", err)
	}
	if st.Event != nil && st.Event.Notify {
		e.out.Enqueue(Job{Scope: s, Event: id, Channels: chans, Message: message(r, h, *st.Event)})
	}
	return nil
}

// observe builds the observation for (rule, host) over the bucket.
func observe(r domain.AlertRule, h domain.Host, buckets map[domain.HostID]query.RollupBucket, o Observation) Observation {
	if r.Kind == domain.RuleNoData {
		last := h.LastSeen
		if last.IsZero() {
			last = h.CreatedAt // never seen: silent since enrolment
		}
		o.SilentFor = max(o.End.Sub(last), 0)
		return o // no value: a no_data event carries none
	}
	if b, ok := buckets[h.ID]; ok {
		o.Value, o.HasValue = query.BucketValue(b, r.Metric, string(r.Aggregation))
	}
	return o
}

func enabled(rules []domain.AlertRule) []domain.AlertRule {
	var out []domain.AlertRule
	for _, r := range rules {
		if r.Enabled {
			out = append(out, r)
		}
	}
	return out
}

// targets are the enabled hosts a rule applies to. A disabled host is never
// evaluated (OLD C-M2): it cannot fire, and a firing alert on it stays as it
// was rather than resolving on missing data.
func targets(r domain.AlertRule, hosts []domain.Host) []domain.Host {
	want := map[domain.HostID]bool{}
	for _, id := range r.Hosts {
		want[id] = true
	}
	var out []domain.Host
	for _, h := range hosts {
		if h.Enabled() && (len(want) == 0 || want[h.ID]) {
			out = append(out, h)
		}
	}
	return out
}

func channelsOf(r domain.AlertRule, all map[int64]domain.Channel) []domain.Channel {
	var out []domain.Channel
	for _, id := range r.Channels {
		if c, ok := all[id]; ok {
			out = append(out, c)
		}
	}
	return out
}

func silenced(sils []domain.Silence, rule int64, host domain.HostID, at time.Time) bool {
	for _, s := range sils {
		if s.Covers(rule, host, at) {
			return true
		}
	}
	return false
}

func message(r domain.AlertRule, h domain.Host, ev domain.AlertEvent) Message {
	return Message{Rule: r.Name, Host: h.Name, State: ev.To, Metric: r.Metric, Value: ev.Value, At: ev.At,
		Resolved: ev.To == domain.StateOK}
}
