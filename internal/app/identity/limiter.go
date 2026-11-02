package identity

import (
	"sync"
	"time"
)

// LoginLimiter throttles failed logins per key. Its state is bounded: an
// unbounded map, or an O(n²) prune of it, would itself be a DoS. When it is
// full even after pruning it fails *closed* by default: keys it cannot track
// are refused until entries expire, since failing open would let an attacker
// fill the table and then guess unthrottled. A table whose keys an attacker
// chooses can instead be filled to lock everyone out, so LoginGuard makes
// those tables fail open and relies on its per-account table, which only
// holds real accounts, to bound guessing.
type LoginLimiter struct {
	mu        sync.Mutex
	failures  map[string]*failure
	limit     int           // failures allowed per window
	window    time.Duration // failure counting window, also the lockout length
	max       int           // tracked keys
	now       func() time.Time
	lastSwept time.Time
	failOpen  bool // when full, admit untracked keys (see LoginGuard)
}

type failure struct {
	count int
	first time.Time
}

// NewLoginLimiter allows limit failures per key per window.
func NewLoginLimiter(limit int, window time.Duration, max int, now func() time.Time) *LoginLimiter {
	return &LoginLimiter{failures: map[string]*failure{}, limit: limit, window: window, max: max, now: now}
}

// FailOpen makes a full limiter admit keys it cannot track instead of
// refusing them. Only safe where another limiter bounds what that admits.
func (l *LoginLimiter) FailOpen() *LoginLimiter {
	l.failOpen = true
	return l
}

// Allowed reports whether key may attempt a login now.
func (l *LoginLimiter) Allowed(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, ok := l.failures[key]
	if !ok {
		return len(l.failures) < l.max || l.sweepAndHasRoom(l.now()) || l.failOpen
	}
	if l.now().Sub(f.first) >= l.window {
		delete(l.failures, key)
		return true
	}
	return f.count < l.limit
}

// Fail records a failed attempt.
func (l *LoginLimiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if f, ok := l.failures[key]; ok && now.Sub(f.first) < l.window {
		f.count++
		return
	}
	if len(l.failures) >= l.max {
		l.sweep(now)
		if len(l.failures) >= l.max {
			return
		}
	}
	l.failures[key] = &failure{count: 1, first: now}
}

// Succeed clears a key after a successful login.
func (l *LoginLimiter) Succeed(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}

func (l *LoginLimiter) sweepAndHasRoom(now time.Time) bool {
	l.sweep(now)
	return len(l.failures) < l.max
}

// sweep drops expired entries, at most once per window/10 so a flood of
// distinct keys cannot make every Fail an O(n) scan (amortised).
func (l *LoginLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSwept) < l.window/10 {
		return
	}
	l.lastSwept = now
	for k, f := range l.failures {
		if now.Sub(f.first) >= l.window {
			delete(l.failures, k)
		}
	}
}

// size is the number of tracked keys (tests).
func (l *LoginLimiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.failures)
}
