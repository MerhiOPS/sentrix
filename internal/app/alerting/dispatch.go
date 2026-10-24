package alerting

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Delivery outcomes recorded on an event.
const (
	OutcomeDelivered = "delivered"
	OutcomeFailed    = "failed"
)

// Attempts per channel, and the waits between them.
var retryBackoff = []time.Duration{2 * time.Second, 10 * time.Second}

// QueueSize bounds undelivered notifications held in memory.
const QueueSize = 256

// Job is one event's notification to its channels.
type Job struct {
	Scope    domain.Scope
	Event    int64
	Channels []domain.Channel
	Message  Message
}

// Dispatcher delivers notifications off the evaluation path: a slow or dead
// webhook must not delay the next minute's evaluation. The event row already
// exists when a job is queued, so a lost job is a visible undelivered event,
// never a silently missing alert.
type Dispatcher struct {
	notifier Notifier
	states   StateRepo
	log      *slog.Logger
	sleep    func(context.Context, time.Duration) error
	queue    chan Job
}

// NewDispatcher builds a dispatcher; Run starts delivering.
func NewDispatcher(n Notifier, states StateRepo, log *slog.Logger) *Dispatcher {
	return &Dispatcher{notifier: n, states: states, log: log, sleep: sleepCtx, queue: make(chan Job, QueueSize)}
}

// Enqueue queues a job. When the queue is full the event is marked failed
// at once rather than blocking evaluation.
func (d *Dispatcher) Enqueue(j Job) {
	select {
	case d.queue <- j:
	default:
		d.record(context.Background(), j, OutcomeFailed, "notification queue full")
	}
}

// Run delivers, one job at a time, until ctx ends. Jobs still queued at
// shutdown keep an empty outcome: visibly undelivered.
func (d *Dispatcher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-d.queue:
			d.Deliver(ctx, j)
		}
	}
}

// Deliver sends one job to every channel and records the outcome. Any
// channel failing marks the event failed (OLD C-M6); the failures say which.
func (d *Dispatcher) Deliver(ctx context.Context, j Job) {
	var fails []string
	for _, ch := range j.Channels {
		if err := d.send(ctx, ch, j.Message); err != nil {
			fails = append(fails, ch.Name+": "+err.Error())
		}
	}
	outcome := OutcomeDelivered
	if len(fails) > 0 {
		outcome = OutcomeFailed
	}
	d.record(ctx, j, outcome, strings.Join(fails, "; "))
}

func (d *Dispatcher) send(ctx context.Context, ch domain.Channel, m Message) error {
	var err error
	for attempt := 0; ; attempt++ {
		if err = d.notifier.Notify(ctx, ch, m); err == nil || attempt == len(retryBackoff) {
			return err
		}
		if d.sleep(ctx, retryBackoff[attempt]) != nil {
			return err
		}
	}
}

func (d *Dispatcher) record(ctx context.Context, j Job, outcome, detail string) {
	// The outcome must land even when shutdown cancelled the delivery.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := d.states.SetDelivery(ctx, j.Scope, j.Event, outcome, detail); err != nil {
		d.log.Error("record alert delivery", "event", j.Event, "err", err)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
