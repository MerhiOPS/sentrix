package uptime

import (
	"context"
	"sync"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// referenceTTL is how long a reference verdict is reused: during an
// outage every failing check asks at once.
const referenceTTL = time.Minute

// References are operator-chosen targets that should always answer. When a
// check fails at the network level and no reference answers either, the
// server's own network is down and the result is unknown, not an incident:
// one lost uplink must not page every tenant.
type References struct {
	probe   Prober
	targets []domain.Monitor
	now     func() time.Time

	mu       sync.Mutex
	checked  time.Time
	verdict  bool
	ttl      time.Duration
	attempts int // probes run, for tests and /system
}

// NewReferences builds the reference check for urls (http or https). Any
// HTTP response counts: it proves the network path, not the site.
func NewReferences(probe Prober, urls []string, now func() time.Time) *References {
	r := &References{probe: probe, now: now, ttl: referenceTTL}
	for _, u := range urls {
		r.targets = append(r.targets, domain.Monitor{Kind: domain.MonitorHTTP, Target: u, Method: "HEAD",
			Timeout: 5 * time.Second, AcceptStatus: []domain.StatusRange{{From: 100, To: 599}}})
	}
	return r
}

// Reachable reports whether any reference answers, reusing a verdict for a
// minute. Concurrent callers wait for one probe rather than each sending
// their own.
func (r *References) Reachable(ctx context.Context) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.checked.IsZero() && r.now().Sub(r.checked) < r.ttl {
		return r.verdict
	}
	r.verdict = false
	for _, t := range r.targets {
		r.attempts++
		if r.probe.Probe(ctx, t).Outcome == domain.CheckUp {
			r.verdict = true
			break
		}
	}
	r.checked = r.now()
	return r.verdict
}

// Configured reports whether any reference target is set.
func (r *References) Configured() bool { return r != nil && len(r.targets) > 0 }
