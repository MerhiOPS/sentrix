package storetest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

func alertRule(hosts []domain.HostID, channels []int64) domain.AlertRule {
	return domain.AlertRule{Name: "cpu high", Kind: domain.RuleThreshold, Metric: "cpu", Aggregation: domain.AggMax,
		Comparator: domain.CmpGT, Threshold: 90.5, Duration: 5 * time.Minute, Hosts: hosts, OnNoData: domain.NoDataResolve,
		Cooldown: 15 * time.Minute, Channels: channels, Enabled: true}
}

func mustChannel(t *testing.T, st Stores, s domain.Scope, name string) int64 {
	t.Helper()
	id, err := st.CreateChannel(context.Background(), s, domain.Channel{Name: name, Kind: domain.ChannelSlack, URL: "https://hooks.example/" + name})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testAlertRuleRoundTrip(t *testing.T, st Stores) {
	ctx := context.Background()
	f := newFixture(t, st, "a")
	other, _ := enroll(t, st, f.scope, "b-host")
	c1, c2 := mustChannel(t, st, f.scope, "one"), mustChannel(t, st, f.scope, "two")
	want := alertRule([]domain.HostID{f.host}, []int64{c1})
	id, err := st.CreateRule(ctx, f.scope, want)
	if err != nil {
		t.Fatal(err)
	}
	want.ID = id
	got, err := st.ListRules(ctx, f.scope)
	if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("ListRules = %+v, %v; want %+v", got, err, want)
	}
	want.Hosts, want.Channels, want.Enabled, want.Kind = []domain.HostID{other.ID}, []int64{c2}, false, domain.RuleNoData
	if err := st.UpdateRule(ctx, f.scope, want); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.ListRules(ctx, f.scope); len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("after update ListRules = %+v; want links replaced: %+v", got, want)
	}
	if err := st.DeleteChannel(ctx, f.scope, c2); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.ListRules(ctx, f.scope); len(got[0].Channels) != 0 {
		t.Fatalf("a deleted channel is still linked: %v", got[0].Channels)
	}
	if err := st.UpdateRule(ctx, f.scope, domain.AlertRule{ID: id + 999}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("update of an unknown rule: %v", err)
	}
}

// A rule naming a host or channel that does not exist in the tenant is
// refused whole: no rule row survives a bad link.
func testAlertRuleUnknownRefsRefused(t *testing.T, st Stores) {
	ctx := context.Background()
	f := newFixture(t, st, "a")
	for _, r := range []domain.AlertRule{
		alertRule([]domain.HostID{f.host, domain.NewHostID()}, nil),
		alertRule([]domain.HostID{f.host}, []int64{424242}),
	} {
		if _, err := st.CreateRule(ctx, f.scope, r); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("CreateRule with an unknown ref: %v, want ErrNotFound", err)
		}
	}
	if got, _ := st.ListRules(ctx, f.scope); len(got) != 0 {
		t.Fatalf("a refused rule was stored: %+v", got)
	}
}

func testAlertTransitionsAndEvents(t *testing.T, st Stores) {
	ctx := context.Background()
	f := newFixture(t, st, "a")
	rule, err := st.CreateRule(ctx, f.scope, alertRule(nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	pending := domain.RuleHostState{Rule: rule, Host: f.host, State: domain.StatePending, Since: T0}
	if id, err := st.RecordTransition(ctx, f.scope, pending, nil); err != nil || id != 0 {
		t.Fatalf("stateless transition: id %d, %v", id, err)
	}
	notified, v := T0.Add(5*time.Minute), 97.25
	firing := domain.RuleHostState{Rule: rule, Host: f.host, State: domain.StateFiring, Since: T0, LastNotified: &notified}
	ev := &domain.AlertEvent{From: domain.StatePending, To: domain.StateFiring, At: notified, Value: &v, Notify: true}
	id, err := st.RecordTransition(ctx, f.scope, firing, ev)
	if err != nil || id == 0 {
		t.Fatalf("RecordTransition: id %d, %v", id, err)
	}
	states, err := st.AlertStates(ctx, f.scope)
	if err != nil || len(states) != 1 || !reflect.DeepEqual(states[0], firing) {
		t.Fatalf("AlertStates = %+v, %v; want %+v", states, err, firing)
	}
	if err := st.SetDelivery(ctx, f.scope, id, "failed", "ops: refused"); err != nil {
		t.Fatal(err)
	}
	resolved := &domain.AlertEvent{From: domain.StateFiring, To: domain.StateOK, At: notified.Add(time.Minute)}
	if _, err := st.RecordTransition(ctx, f.scope, domain.RuleHostState{Rule: rule, Host: f.host, State: domain.StateOK,
		Since: resolved.At, LastNotified: &notified}, resolved); err != nil {
		t.Fatal(err)
	}
	evs, err := st.ListEvents(ctx, f.scope, 10)
	if err != nil || len(evs) != 2 {
		t.Fatalf("ListEvents = %+v, %v", evs, err)
	}
	first := evs[1]
	if evs[0].To != domain.StateOK || evs[0].Value != nil || first.Value == nil || *first.Value != v ||
		first.Outcome != "failed" || first.Delivery != "ops: refused" || !first.Notify || first.Host != f.host || first.Rule != rule {
		t.Fatalf("events %+v / %+v: want newest first, values and delivery kept", evs[0], first)
	}
	if one, _ := st.ListEvents(ctx, f.scope, 1); len(one) != 1 {
		t.Fatalf("limit ignored: %d events", len(one))
	}
	if err := st.DeleteRule(ctx, f.scope, rule); err != nil {
		t.Fatal(err)
	}
	states, _ = st.AlertStates(ctx, f.scope)
	evs, _ = st.ListEvents(ctx, f.scope, 10)
	if len(states)+len(evs) != 0 {
		t.Fatalf("deleting a rule left %d states and %d events", len(states), len(evs))
	}
}

func testAlertSilenceRoundTrip(t *testing.T, st Stores) {
	ctx := context.Background()
	f := newFixture(t, st, "a")
	rule, _ := st.CreateRule(ctx, f.scope, alertRule(nil, nil))
	host := f.host
	want := []domain.Silence{
		{Starts: T0, Ends: T0.Add(time.Hour), Reason: "everything"},
		{Rule: &rule, Host: &host, Starts: T0, Ends: T0.Add(2 * time.Hour), Reason: "patching"},
	}
	for i := range want {
		id, err := st.CreateSilence(ctx, f.scope, want[i])
		if err != nil {
			t.Fatal(err)
		}
		want[i].ID = id
	}
	got, err := st.ListSilences(ctx, f.scope)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("ListSilences = %+v, %v; want %+v", got, err, want)
	}
	if _, err := st.CreateSilence(ctx, f.scope, domain.Silence{Host: ptr(domain.NewHostID()), Starts: T0, Ends: T0.Add(time.Hour)}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("silence for an unknown host: %v", err)
	}
	if err := st.DeleteSilence(ctx, f.scope, want[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteSilence(ctx, f.scope, want[0].ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }

// TenantBuckets must agree with Rollups for every host: the evaluator and
// the chart read the same bucket.
func testTenantBucketsMatchRollups(t *testing.T, st Stores) {
	ctx := context.Background()
	f := newFixture(t, st, "a")
	quiet, _ := enroll(t, st, f.scope, "quiet")
	if _, err := st.CommitBatch(ctx, f.scope, f.host, 1, prepared(T0, []int{0, 1, 2, 13}, []uint64{10, 20, 40, 90})); err != nil {
		t.Fatal(err)
	}
	got, err := st.TenantBuckets(ctx, f.scope, T0)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := st.Rollups(ctx, f.scope, f.host, query.Res1m, T0, T0.Add(time.Minute))
	if len(want) != 1 || !reflect.DeepEqual(got[f.host], want[0]) {
		t.Fatalf("TenantBuckets = %+v; Rollups = %+v", got[f.host], want)
	}
	if _, ok := got[quiet.ID]; ok || len(got) != 1 {
		t.Fatalf("a host without data has a bucket: %+v", got)
	}
}

func testHostDisableAndList(t *testing.T, st Stores) {
	ctx := context.Background()
	f := newFixture(t, st, "a")
	at := T0.Add(time.Minute)
	if err := st.SetHostDisabled(ctx, f.scope, f.host, &at); err != nil {
		t.Fatal(err)
	}
	h, _ := st.GetHost(ctx, f.scope, f.host)
	ah, err := st.AuthenticateCredential(ctx, f.credHash)
	if h.DisabledAt == nil || !h.DisabledAt.Equal(at) || err != nil || ah.Enabled {
		t.Fatalf("after disable: host %v, auth enabled %v (%v)", h.DisabledAt, ah.Enabled, err)
	}
	if err := st.SetHostDisabled(ctx, f.scope, f.host, nil); err != nil {
		t.Fatal(err)
	}
	if h, _ := st.GetHost(ctx, f.scope, f.host); h.DisabledAt != nil {
		t.Fatal("re-enable did not clear disabled_at")
	}
	if err := st.SetHostDisabled(ctx, f.scope, domain.NewHostID(), &at); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("disable unknown host: %v", err)
	}
	second := newFixture(t, st, "b")
	ids, err := st.ListTenants(ctx, mustOps(t))
	has := func(id domain.TenantID) bool { return len(ids) == 2 && (ids[0] == id || ids[1] == id) }
	if err != nil || !has(f.tenant) || !has(second.tenant) {
		t.Fatalf("ListTenants = %v, %v", ids, err)
	}
	if _, err := st.ListTenants(ctx, domain.OpsScope{}); err == nil {
		t.Fatal("ListTenants accepted a zero OpsScope")
	}
}
