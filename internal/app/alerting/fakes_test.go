package alerting

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// fake is one tenant's alerting world; it ignores the scope because the
// evaluator's tenant handling is tested by the storage contract.
type fake struct {
	mu       sync.Mutex
	tenant   domain.TenantID
	hosts    []domain.Host
	rules    []domain.AlertRule
	channels []domain.Channel
	silences []domain.Silence
	buckets  map[time.Time]map[domain.HostID]query.RollupBucket
	states   map[stateKey]domain.RuleHostState
	events   []domain.AlertEvent
	// order records "event" and "state" writes, to check R1's ordering.
	order []string
	// bucketReads counts TenantBuckets calls.
	bucketReads int
}

func newFake() *fake {
	return &fake{tenant: domain.NewTenantID(), buckets: map[time.Time]map[domain.HostID]query.RollupBucket{},
		states: map[stateKey]domain.RuleHostState{}}
}

func (f *fake) deps() Deps {
	return Deps{Tenants: f, Hosts: f, Rules: f, Channels: f, Silences: f, States: f, Buckets: f}
}

// cpu stores a bucket where every sample of host had value v.
func (f *fake) cpu(start time.Time, host domain.HostID, v float64) {
	if f.buckets[start] == nil {
		f.buckets[start] = map[domain.HostID]query.RollupBucket{}
	}
	f.buckets[start][host] = query.RollupBucket{Start: start, N: 1, Gauges: map[query.Gauge]query.Agg{"cpu": {Sum: v, Min: v, Max: v}},
		FirstTS: start, LastTS: start}
}

func (f *fake) ListTenants(context.Context, domain.OpsScope) ([]domain.TenantID, error) {
	return []domain.TenantID{f.tenant}, nil
}
func (f *fake) GetHost(_ context.Context, _ domain.Scope, id domain.HostID) (domain.Host, error) {
	for _, h := range f.hosts {
		if h.ID == id {
			return h, nil
		}
	}
	return domain.Host{}, domain.ErrNotFound
}
func (f *fake) ListHosts(context.Context, domain.Scope) ([]domain.Host, error) { return f.hosts, nil }
func (f *fake) ListRules(context.Context, domain.Scope) ([]domain.AlertRule, error) {
	return f.rules, nil
}
func (f *fake) CreateRule(context.Context, domain.Scope, domain.AlertRule) (int64, error) {
	return 0, errors.New("unused")
}
func (f *fake) UpdateRule(context.Context, domain.Scope, domain.AlertRule) error {
	return errors.New("unused")
}
func (f *fake) DeleteRule(context.Context, domain.Scope, int64) error { return errors.New("unused") }
func (f *fake) ListChannels(context.Context, domain.Scope) ([]domain.Channel, error) {
	return f.channels, nil
}
func (f *fake) CreateChannel(context.Context, domain.Scope, domain.Channel) (int64, error) {
	return 0, errors.New("unused")
}
func (f *fake) UpdateChannel(context.Context, domain.Scope, domain.Channel) error {
	return errors.New("unused")
}
func (f *fake) DeleteChannel(context.Context, domain.Scope, int64) error { return errors.New("unused") }
func (f *fake) ListSilences(context.Context, domain.Scope) ([]domain.Silence, error) {
	return f.silences, nil
}
func (f *fake) CreateSilence(context.Context, domain.Scope, domain.Silence) (int64, error) {
	return 0, errors.New("unused")
}
func (f *fake) DeleteSilence(context.Context, domain.Scope, int64) error { return errors.New("unused") }
func (f *fake) TenantBuckets(_ context.Context, _ domain.Scope, start time.Time) (map[domain.HostID]query.RollupBucket, error) {
	f.bucketReads++
	return f.buckets[start], nil
}
func (f *fake) AlertStates(context.Context, domain.Scope) ([]domain.RuleHostState, error) {
	var out []domain.RuleHostState
	for _, s := range f.states {
		out = append(out, s)
	}
	return out, nil
}
func (f *fake) RecordTransition(_ context.Context, _ domain.Scope, st domain.RuleHostState, ev *domain.AlertEvent) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var id int64
	if ev != nil {
		e := *ev
		e.ID = int64(len(f.events) + 1)
		id = e.ID
		f.events = append(f.events, e)
		f.order = append(f.order, "event")
	}
	f.states[stateKey{st.Rule, st.Host}] = st
	f.order = append(f.order, "state")
	return id, nil
}
func (f *fake) SetDelivery(_ context.Context, _ domain.Scope, id int64, outcome, detail string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events[id-1].Outcome, f.events[id-1].Delivery = outcome, detail
	return nil
}
func (f *fake) ListEvents(context.Context, domain.Scope, int) ([]domain.AlertEvent, error) {
	return f.events, nil
}

// notifier records messages and fails the first fails calls.
type notifier struct {
	mu    sync.Mutex
	calls int
	fails int
	sent  []Message
}

func (n *notifier) Notify(_ context.Context, _ domain.Channel, m Message) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls++
	if n.calls <= n.fails {
		return errors.New("connection refused")
	}
	n.sent = append(n.sent, m)
	return nil
}
