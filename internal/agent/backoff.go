package agent

import (
	"math/rand/v2"
	"time"
)

// Backoff is exponential backoff with full jitter: the wait is uniform in
// [0, min(max, base·2^attempt)]. Jitter matters more than the exponent — when
// a server restarts, every agent's retry timer started at the same moment,
// and without jitter they would all reconnect in the same instant.
type Backoff struct {
	Base    time.Duration
	Max     time.Duration
	attempt int
	rnd     func() float64
}

// NewBackoff builds a backoff with the given bounds.
func NewBackoff(base, max time.Duration) *Backoff {
	return &Backoff{Base: base, Max: max, rnd: rand.Float64}
}

// Next returns the next wait and advances the attempt counter.
func (b *Backoff) Next() time.Duration {
	ceiling := b.Base << min(b.attempt, 30)
	if ceiling <= 0 || ceiling > b.Max {
		ceiling = b.Max
	}
	b.attempt++
	return time.Duration(b.rnd() * float64(ceiling))
}

// Reset starts over after a success.
func (b *Backoff) Reset() { b.attempt = 0 }
