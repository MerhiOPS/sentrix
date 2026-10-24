package ingest

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Authenticator resolves agent credentials with a small cache, so a host
// sending every 10 s does not cost a database read per batch.
//
// Lookup is by SHA-256 of the 32-byte random credential. A timing difference
// in that lookup can reveal, at most, something about a hash prefix — not
// the secret — so the usual constant-time comparison buys nothing here and is
// deliberately not performed (PLAN Phase 1 asked for it; LOG.md says why not).
type Authenticator struct {
	store CredentialStore
	ttl   time.Duration
	max   int
	now   func() time.Time

	mu    sync.Mutex
	cache map[[sha256.Size]byte]cached
}

type cached struct {
	host    AuthHost
	expires time.Time
}

// NewAuthenticator builds the cache. ttl bounds how long a disabled host can
// keep sending after Invalidate was missed; max bounds memory.
func NewAuthenticator(store CredentialStore, ttl time.Duration, max int, now func() time.Time) *Authenticator {
	return &Authenticator{store: store, ttl: ttl, max: max, now: now, cache: map[[sha256.Size]byte]cached{}}
}

// Authenticate returns the host owning credential, or ErrUnauthenticated.
func (a *Authenticator) Authenticate(ctx context.Context, credential []byte) (AuthHost, error) {
	key := sha256.Sum256(credential)
	now := a.now()
	if h, ok := a.lookup(key, now); ok {
		return h, nil
	}
	h, err := a.store.AuthenticateCredential(ctx, key[:])
	if errors.Is(err, domain.ErrNotFound) {
		return AuthHost{}, fmt.Errorf("%w: unknown credential", domain.ErrUnauthenticated)
	}
	if err != nil {
		// Not "unauthenticated": a 401 tells the agent to stop shipping, so a
		// database hiccup must surface as a server error the agent retries.
		return AuthHost{}, fmt.Errorf("authenticate: %w", err)
	}
	a.remember(key, h, now)
	return h, nil
}

// Invalidate forgets every cached credential of host (call on disable).
func (a *Authenticator) Invalidate(host domain.HostID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, c := range a.cache {
		if c.host.Host == host {
			delete(a.cache, k)
		}
	}
}

func (a *Authenticator) lookup(key [sha256.Size]byte, now time.Time) (AuthHost, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.cache[key]
	if !ok || now.After(c.expires) {
		delete(a.cache, key)
		return AuthHost{}, false
	}
	return c.host, true
}

func (a *Authenticator) remember(key [sha256.Size]byte, h AuthHost, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.cache) >= a.max {
		a.evictExpired(now)
	}
	if len(a.cache) >= a.max {
		for k := range a.cache { // still full: drop an arbitrary entry
			delete(a.cache, k)
			break
		}
	}
	a.cache[key] = cached{host: h, expires: now.Add(a.ttl)}
}

func (a *Authenticator) evictExpired(now time.Time) {
	for k, c := range a.cache {
		if now.After(c.expires) {
			delete(a.cache, k)
		}
	}
}
