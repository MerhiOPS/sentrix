package ingesthttp

import (
	"math"
	"sync"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Per-host request rate. An agent sends one batch every 10 s and replays a
// backlog at 10 batches/s; the cap only stops a misbehaving or hostile
// client from monopolising the single writer.
const (
	HostRate    = 20.0 // requests per second, sustained
	HostBurst   = 40.0 // requests allowed at once
	maxTracked  = 100_000
	idleForgets = 10 * time.Minute
)

// limiter is a token bucket per host. A refused request stores nothing and
// is answered 429 with Retry-After; the agent keeps the batch and resends.
type limiter struct {
	mu      sync.Mutex
	rate    float64
	burst   float64
	now     func() time.Time
	buckets map[domain.HostID]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newLimiter(rate, burst float64, now func() time.Time) *limiter {
	return &limiter{rate: rate, burst: burst, now: now, buckets: map[domain.HostID]*bucket{}}
}

// allow takes a token for host, or returns how long until one is available.
func (l *limiter) allow(host domain.HostID) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[host]
	if !ok {
		if len(l.buckets) >= maxTracked {
			l.forgetIdle(now)
		}
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[host] = b
	}
	b.tokens = math.Min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
	return false, wait
}

// forgetIdle drops buckets that have refilled completely (they carry no
// state worth keeping). Caller holds mu.
func (l *limiter) forgetIdle(now time.Time) {
	for h, b := range l.buckets {
		if now.Sub(b.last) > idleForgets {
			delete(l.buckets, h)
		}
	}
	if len(l.buckets) >= maxTracked { // every host active: start over rather than grow
		l.buckets = map[domain.HostID]*bucket{}
	}
}

// retryAfterSeconds renders a wait for the Retry-After header: whole
// seconds, at least 1.
func retryAfterSeconds(d time.Duration) int {
	return max(1, int(math.Ceil(d.Seconds())))
}
