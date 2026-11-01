package uptime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/alerting"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// fakes for the checker's ports. Embedding the interfaces keeps them short:
// a method the checker should not call panics.
type fakeProbe struct {
	results []domain.CheckResult
	calls   int
}

func (f *fakeProbe) Probe(context.Context, domain.Monitor) domain.CheckResult {
	r := f.results[min(f.calls, len(f.results)-1)]
	f.calls++
	return r
}

type fakeStates struct {
	StateRepo
	recorded  []CheckRecord
	err       error
	delivered map[int64]string
}

func (f *fakeStates) RecordCheck(_ context.Context, _ domain.Scope, rec CheckRecord) ([]int64, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.recorded = append(f.recorded, rec)
	ids := make([]int64, len(rec.Step.Events))
	for i := range ids {
		ids[i] = int64(100 + i)
	}
	return ids, nil
}
func (f *fakeStates) SetMonitorDelivery(_ context.Context, _ domain.Scope, ev int64, outcome, _ string) error {
	f.delivered[ev] = outcome
	return nil
}

type fakeConfig struct {
	alerting.ChannelRepo
	alerting.SilenceRepo
	channels []domain.Channel
	silences []domain.Silence
}

func (f *fakeConfig) ListChannels(context.Context, domain.Scope) ([]domain.Channel, error) {
	return f.channels, nil
}
func (f *fakeConfig) ListSilences(context.Context, domain.Scope) ([]domain.Silence, error) {
	return f.silences, nil
}

type fakeOut struct{ jobs []alerting.Job }

func (f *fakeOut) Enqueue(j alerting.Job) { f.jobs = append(f.jobs, j) }

type rig struct {
	probe  *fakeProbe
	refs   *fakeProbe
	states *fakeStates
	cfg    *fakeConfig
	out    *fakeOut
	c      *Checker
}

func newRig(refURLs ...string) *rig {
	r := &rig{probe: &fakeProbe{}, refs: &fakeProbe{results: []domain.CheckResult{{Outcome: domain.CheckDown}}},
		states: &fakeStates{delivered: map[int64]string{}}, out: &fakeOut{},
		cfg: &fakeConfig{channels: []domain.Channel{{ID: 3, Name: "ops"}, {ID: 4, Name: "other"}}}}
	r.c = NewChecker(CheckerDeps{Probe: r.probe, States: r.states, Channels: r.cfg, Silences: r.cfg, Out: r.out,
		Refs: NewReferences(r.refs, refURLs, func() time.Time { return t0 })})
	return r
}

// downNow is a monitor whose next failed check confirms an incident.
func downNow() Scheduled {
	return Scheduled{Tenant: domain.NewTenantID(), Monitor: domain.Monitor{ID: 7, Name: "shop", Target: "https://shop.example.com",
		Interval: time.Minute, Channels: []int64{3}}, State: domain.MonitorState{Monitor: 7, Status: domain.MonitorUp, Revision: 5}}
}

func failed(c domain.ErrorClass) domain.CheckResult {
	return domain.CheckResult{At: t0, Outcome: domain.CheckDown, Class: c}
}

func TestConfirmedDownIsNotifiedOnTheMonitorsChannels(t *testing.T) {
	r := newRig()
	r.probe.results = []domain.CheckResult{failed(domain.ErrStatus)}
	next, err := r.c.Check(context.Background(), downNow())
	if err != nil || next.Status != domain.MonitorDown || next.Revision != 6 {
		t.Fatalf("next %+v, %v", next, err)
	}
	rec := r.states.recorded[0]
	if rec.Revision != 5 || !rec.Step.Events[0].Notify {
		t.Fatalf("record %+v: revision 5 and a notified down event", rec)
	}
	if len(r.out.jobs) != 1 || len(r.out.jobs[0].Channels) != 1 || r.out.jobs[0].Channels[0].ID != 3 || r.out.jobs[0].Event != 100 {
		t.Fatalf("jobs %+v: one job, to the monitor's channel only, for the stored event", r.out.jobs)
	}
	j := r.out.jobs[0]
	if m := j.Message.Monitor; m == nil || m.Event != domain.EventDown || m.Class != domain.ErrStatus || m.Name != "shop" {
		t.Fatalf("message %+v", j.Message)
	}
	if err := j.Recorder.SetDelivery(context.Background(), j.Scope, j.Event, alerting.OutcomeDelivered, ""); err != nil ||
		r.states.delivered[100] != alerting.OutcomeDelivered {
		t.Fatal("the job's recorder must store the outcome on the monitor event")
	}
}

func TestSilencesAndMissingChannelsSuppressNotification(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(*rig, *Scheduled)
		notify bool
	}{
		{"silenced monitor", func(r *rig, _ *Scheduled) {
			r.cfg.silences = []domain.Silence{{Monitor: ptr(int64(7)), Starts: t0, Ends: t0.Add(time.Hour)}}
		}, false},
		{"silence on everything", func(r *rig, _ *Scheduled) {
			r.cfg.silences = []domain.Silence{{Starts: t0.Add(-time.Hour), Ends: t0.Add(time.Hour)}}
		}, false},
		{"no channels", func(_ *rig, s *Scheduled) { s.Monitor.Channels = nil }, false},
		{"channel deleted", func(r *rig, _ *Scheduled) { r.cfg.channels = nil }, false},
		{"an alert rule's silence", func(r *rig, _ *Scheduled) {
			r.cfg.silences = []domain.Silence{{Rule: ptr(int64(1)), Starts: t0, Ends: t0.Add(time.Hour)}}
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig()
			r.probe.results = []domain.CheckResult{failed(domain.ErrStatus)}
			s := downNow()
			c.setup(r, &s)
			if _, err := r.c.Check(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			if got := r.states.recorded[0].Step.Events[0].Notify; got != c.notify || (len(r.out.jobs) == 1) != c.notify {
				t.Fatalf("notify %v, jobs %d, want notified=%v", got, len(r.out.jobs), c.notify)
			}
			if r.states.recorded[0].Step.Next.Status != domain.MonitorDown {
				t.Fatal("suppressing a notification must not stop the transition")
			}
		})
	}
}

func TestReferencesTurnServerOutagesIntoUnknown(t *testing.T) {
	r := newRig("https://reference.example/")
	r.probe.results = []domain.CheckResult{failed(domain.ErrConnect)}
	next, err := r.c.Check(context.Background(), downNow())
	if err != nil {
		t.Fatal(err)
	}
	rec := r.states.recorded[0]
	if rec.Result.Outcome != domain.CheckUnknown || len(rec.Step.Events) != 0 || next.Status != domain.MonitorUp || len(r.out.jobs) != 0 {
		t.Fatalf("with references down a connect failure must be unknown: %+v", rec)
	}
	// A status failure is the site's own answer: references are not asked.
	r2 := newRig("https://reference.example/")
	r2.probe.results = []domain.CheckResult{failed(domain.ErrStatus)}
	_, _ = r2.c.Check(context.Background(), downNow())
	if r2.refs.calls != 0 || r2.states.recorded[0].Result.Outcome != domain.CheckDown {
		t.Fatal("an HTTP status failure consulted the references")
	}
	// References up: the failure is real.
	r3 := newRig("https://reference.example/")
	r3.refs.results = []domain.CheckResult{{Outcome: domain.CheckUp}}
	r3.probe.results = []domain.CheckResult{failed(domain.ErrTimeout)}
	_, _ = r3.c.Check(context.Background(), downNow())
	if r3.states.recorded[0].Result.Outcome != domain.CheckDown {
		t.Fatal("with a reference answering, a timeout is the target's")
	}
	// No references configured: never unknown.
	r4 := newRig()
	r4.probe.results = []domain.CheckResult{failed(domain.ErrConnect)}
	_, _ = r4.c.Check(context.Background(), downNow())
	if r4.states.recorded[0].Result.Outcome != domain.CheckDown {
		t.Fatal("without references a failure must count")
	}
}

func TestReferenceVerdictIsCached(t *testing.T) {
	probe := &fakeProbe{results: []domain.CheckResult{{Outcome: domain.CheckDown}}}
	now := t0
	refs := NewReferences(probe, []string{"https://a.example/", "https://b.example/"}, func() time.Time { return now })
	for range 5 {
		if refs.Reachable(context.Background()) {
			t.Fatal("unreachable references reported reachable")
		}
	}
	if probe.calls != 2 {
		t.Fatalf("%d probes for five calls in one minute, want one round of 2", probe.calls)
	}
	now = now.Add(time.Minute)
	probe.results = []domain.CheckResult{{Outcome: domain.CheckUp}}
	if !refs.Reachable(context.Background()) || probe.calls != 3 {
		t.Fatalf("after the TTL a fresh probe must run (calls %d)", probe.calls)
	}
}

func TestStaleRecordIsNotNotified(t *testing.T) {
	r := newRig()
	r.probe.results = []domain.CheckResult{failed(domain.ErrStatus)}
	r.states.err = ErrStale
	s := downNow()
	next, err := r.c.Check(context.Background(), s)
	if !errors.Is(err, ErrStale) || len(r.out.jobs) != 0 || next != s.State {
		t.Fatalf("stale: err %v, jobs %d, next %+v", err, len(r.out.jobs), next)
	}
}

func ptr[T any](v T) *T { return &v }
