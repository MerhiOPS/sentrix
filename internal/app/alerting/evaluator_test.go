package alerting

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// world is a fake tenant with one enabled host, one 2m cpu rule and one
// channel, evaluated on a fake clock.
type world struct {
	f    *fake
	n    *notifier
	d    *Dispatcher
	e    *Evaluator
	host domain.HostID
	now  time.Time
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{f: newFake(), n: &notifier{}, host: domain.NewHostID(), now: t0}
	w.f.hosts = []domain.Host{{ID: w.host, Name: "web-1", CreatedAt: t0.Add(-time.Hour), LastSeen: t0}}
	w.f.channels = []domain.Channel{{ID: 7, Name: "ops", Kind: domain.ChannelWebhook, URL: "https://hooks.example/x"}}
	r := cpuRule(2*time.Minute, domain.NoDataHold)
	r.Channels = []int64{7}
	w.f.rules = []domain.AlertRule{r}
	w.d = NewDispatcher(w.n, w.f, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.d.sleep = func(context.Context, time.Duration) error { return nil }
	w.e = NewEvaluator(w.f.deps(), w.d, func() time.Time { return w.now })
	return w
}

// minute stores cpu v for bucket i and evaluates it at the bucket's end
// plus the offset, delivering what was queued.
func (w *world) minute(t *testing.T, i int, v float64) {
	t.Helper()
	start := t0.Add(time.Duration(i) * time.Minute)
	w.f.cpu(start, w.host, v)
	w.now = start.Add(time.Minute + EvalOffset)
	if err := w.e.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.drain()
}

func (w *world) drain() {
	for {
		select {
		case j := <-w.d.queue:
			w.d.Deliver(context.Background(), j)
		default:
			return
		}
	}
}

func TestEvaluatorFiresNotifiesAndResolves(t *testing.T) {
	w := newWorld(t)
	for i, v := range []float64{95, 95, 95, 10} {
		w.minute(t, i, v)
	}
	if len(w.f.events) != 2 || w.f.events[0].To != domain.StateFiring || w.f.events[1].To != domain.StateOK {
		t.Fatalf("events %+v, want firing then resolve", w.f.events)
	}
	if len(w.n.sent) != 2 || !w.n.sent[1].Resolved || w.n.sent[0].Host != "web-1" {
		t.Fatalf("sent %+v, want a firing and a resolve notification", w.n.sent)
	}
	for _, ev := range w.f.events {
		if ev.Outcome != OutcomeDelivered {
			t.Fatalf("event outcome %q, want delivered", ev.Outcome)
		}
	}
	if w.f.order[0] != "state" || w.f.order[1] != "event" || w.f.order[2] != "state" {
		t.Fatalf("write order %v: the event must be written before its state", w.f.order)
	}
}

// Re-evaluating a bucket is harmless (the machine is idempotent), but the
// evaluator polls every few seconds: a tick must not re-read a bucket it has
// already evaluated.
func TestRepeatedTickEvaluatesOnce(t *testing.T) {
	w := newWorld(t)
	w.minute(t, 0, 95)
	w.minute(t, 1, 95)
	reads, writes := w.f.bucketReads, len(w.f.order)
	for _, back := range []time.Duration{0, 20 * time.Second} { // later in the same minute
		w.now = w.now.Add(back)
		if err := w.e.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if w.f.bucketReads != reads || len(w.f.order) != writes {
		t.Fatalf("repeated ticks re-evaluated: reads %d → %d, writes %d → %d", reads, w.f.bucketReads, writes, len(w.f.order))
	}
}

func TestDisabledHostNeverFires(t *testing.T) {
	w := newWorld(t)
	off := t0
	w.f.hosts[0].DisabledAt = &off
	for i := range 5 {
		w.minute(t, i, 99)
	}
	if len(w.f.events) != 0 || len(w.f.states) != 0 {
		t.Fatalf("a disabled host was evaluated: events %v states %v", w.f.events, w.f.states)
	}
}

func TestDisabledRuleNeverEvaluated(t *testing.T) {
	w := newWorld(t)
	w.f.rules[0].Enabled = false
	for i := range 5 {
		w.minute(t, i, 99)
	}
	if len(w.f.events) != 0 {
		t.Fatalf("a disabled rule fired: %v", w.f.events)
	}
}

func TestSilenceSuppressesNotificationNotTransition(t *testing.T) {
	w := newWorld(t)
	w.f.silences = []domain.Silence{{Host: &w.host, Starts: t0, Ends: t0.Add(time.Hour)}}
	w.minute(t, 0, 95)
	w.minute(t, 1, 95)
	if len(w.f.events) != 1 || w.f.events[0].To != domain.StateFiring {
		t.Fatalf("events %v: a silence must not stop the transition", w.f.events)
	}
	if w.f.events[0].Notify || len(w.n.sent) != 0 {
		t.Fatal("a silenced firing was notified")
	}
}

func TestFailedDeliveryRetriedThenRecorded(t *testing.T) {
	w := newWorld(t)
	w.n.fails = 2
	w.minute(t, 0, 95)
	w.minute(t, 1, 95)
	if w.n.calls != 3 || w.f.events[0].Outcome != OutcomeDelivered {
		t.Fatalf("calls %d outcome %q: want delivered on the third attempt", w.n.calls, w.f.events[0].Outcome)
	}
	w2 := newWorld(t)
	w2.n.fails = 99
	w2.minute(t, 0, 95)
	w2.minute(t, 1, 95)
	ev := w2.f.events[0]
	if w2.n.calls != 3 || ev.Outcome != OutcomeFailed || ev.Delivery != "ops: connection refused" {
		t.Fatalf("calls %d event %+v: want 3 attempts then a visible failure", w2.n.calls, ev)
	}
}

func TestFullQueueMarksEventFailed(t *testing.T) {
	w := newWorld(t)
	for range QueueSize {
		w.d.queue <- Job{}
	}
	w.f.events = append(w.f.events, domain.AlertEvent{ID: 1})
	w.d.Enqueue(Job{Event: 1})
	if w.f.events[0].Outcome != OutcomeFailed {
		t.Fatal("an event dropped from a full queue was not marked failed")
	}
}

func TestNoDataRuleFromLastSeen(t *testing.T) {
	w := newWorld(t)
	w.f.rules = []domain.AlertRule{{ID: 3, Name: "silent", Kind: domain.RuleNoData, Duration: 5 * time.Minute, Enabled: true}}
	w.f.hosts[0].LastSeen = t0
	w.now = t0.Add(4*time.Minute + EvalOffset) // bucket [3m, 4m): silent 4m
	if err := w.e.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.f.events) != 0 {
		t.Fatal("fired before the silence reached the rule's duration")
	}
	w.now = t0.Add(5*time.Minute + EvalOffset)
	if err := w.e.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.f.events) != 1 || w.f.events[0].Value != nil {
		t.Fatalf("events %+v, want one valueless firing", w.f.events)
	}
}
