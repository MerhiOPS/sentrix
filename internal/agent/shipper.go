package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/MerhiOPS/sentrix/internal/wire"
)

// stoppedRecheck is how often a stopped agent (credential refused) tries
// again, so re-enabling a host on the server resumes it without a restart.
const stoppedRecheck = 5 * time.Minute

// Shipper sends the buffer's head until it is acknowledged, in order. FIFO
// is deliberate: the server folds counter resets in batch order.
type Shipper struct {
	client     *http.Client
	endpoint   string
	credential string
	buf        *Buffer
	backoff    *Backoff
	log        *slog.Logger
	now        func() time.Time
	sleep      func(context.Context, time.Duration) bool
	// settled is called when a batch leaves the buffer for good: acked, or
	// refused as permanently invalid. Log positions advance only here.
	settled func(Pending)

	skewMS atomic.Int64
}

// NewShipper builds a shipper for one server identity.
func NewShipper(c *http.Client, id Identity, buf *Buffer, log *slog.Logger) *Shipper {
	return &Shipper{
		client: c, endpoint: id.ServerURL + wire.PathBatch, credential: id.Credential,
		buf: buf, backoff: NewBackoff(time.Second, 5*time.Minute), log: log,
		now: time.Now, sleep: sleepCtx, settled: func(Pending) {},
	}
}

// Run ships until ctx is done.
func (s *Shipper) Run(ctx context.Context) {
	for ctx.Err() == nil {
		p, ok := s.buf.Head()
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-s.buf.Ready():
			}
			continue
		}
		if !s.sleep(ctx, s.handle(ctx, p)) {
			return
		}
	}
}

// Drain sends until the buffer is empty, a send does not succeed, or ctx
// ends. It never waits out a backoff: shutdown is not the time to retry.
// It returns how many batches are left.
func (s *Shipper) Drain(ctx context.Context) int {
	for ctx.Err() == nil {
		p, ok := s.buf.Head()
		if !ok {
			return 0
		}
		before := s.buf.Len()
		s.handle(ctx, p)
		if s.buf.Len() >= before {
			break // not acked or dropped: give up rather than spin
		}
	}
	return s.buf.Len()
}

// handle sends one batch, applies the outcome, and returns how long to wait
// before the next attempt.
func (s *Shipper) handle(ctx context.Context, p Pending) time.Duration {
	resp, err := post(ctx, s.client, s.endpoint, s.credential, p.Body, true)
	if err != nil {
		resp.Status = 0
		s.log.Warn("send failed", "seq", p.Seq, "err", err)
	}
	switch classify(resp.Status) {
	case Acked:
		s.acked(p, resp.Body)
		s.settled(p)
		s.backoff.Reset()
		return 0
	case Dropped:
		s.buf.Discard(p.Seq)
		s.settled(p) // a refused batch is never resent: its lines must not be re-read forever
		s.log.Error("server refused batch permanently; dropped", "seq", p.Seq,
			"status", resp.Status, "reason", serverMessage(resp.Body))
		return 0
	case Stopped:
		s.log.Error("server refused the credential; buffering until it is accepted again",
			"status", resp.Status, "reason", serverMessage(resp.Body), "recheck", stoppedRecheck)
		return stoppedRecheck
	default:
		wait := max(s.backoff.Next(), resp.RetryAfter)
		s.log.Info("server unavailable; will retry", "seq", p.Seq, "status", resp.Status, "in", wait.Round(time.Millisecond))
		return wait
	}
}

func (s *Shipper) acked(p Pending, body []byte) {
	s.buf.Remove(p.Seq)
	var ack wire.Ack
	if json.Unmarshal(body, &ack) != nil {
		return
	}
	skew := s.now().UnixMilli() - ack.ServerTSMS
	s.skewMS.Store(skew)
	if ack.Rejected > 0 {
		s.log.Warn("server rejected samples", "seq", p.Seq, "rejected", ack.Rejected, "clock_skew_ms", skew)
	}
	if abs(skew) > 30_000 {
		s.log.Warn("agent clock differs from server", "skew_ms", skew)
	}
}

// ClockSkew implements Stats: agent clock minus server clock at the last ack.
func (s *Shipper) ClockSkew() time.Duration { return time.Duration(s.skewMS.Load()) * time.Millisecond }

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// sleepCtx waits d or until ctx ends; false means ctx ended.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
