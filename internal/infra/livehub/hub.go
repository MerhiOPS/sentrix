package livehub

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Event is one host's newly stored samples.
type Event struct {
	Host    domain.HostID
	Samples []domain.CoreSample
}

// Sizes: ringSize samples per host (30 min at 5 s), sendBuffer events per
// subscriber before it counts as slow, backfillMax samples per host on
// reconnect.
const (
	ringSize    = 360
	sendBuffer  = 64
	backfillMax = 120
)

type hostKey struct {
	tenant domain.TenantID
	host   domain.HostID
}

// Hub fans stored samples out to subscribers of the same tenant.
type Hub struct {
	mu    sync.RWMutex
	subs  map[*Sub]struct{}
	rings map[hostKey]*ring

	slowDropped atomic.Uint64
}

// New returns an empty hub.
func New() *Hub { return &Hub{subs: map[*Sub]struct{}{}, rings: map[hostKey]*ring{}} }

// PublishCore implements ingest.Publisher. It never blocks: a subscriber
// whose buffer is full is closed and counted, so one slow browser cannot
// stall ingest for everyone.
func (h *Hub) PublishCore(tenant domain.TenantID, host domain.HostID, samples []domain.CoreSample) {
	if len(samples) == 0 {
		return
	}
	cp := append([]domain.CoreSample(nil), samples...)
	h.mu.Lock()
	r, ok := h.rings[hostKey{tenant, host}]
	if !ok {
		r = newRing(ringSize)
		h.rings[hostKey{tenant, host}] = r
	}
	for _, s := range cp {
		r.push(s)
	}
	var slow []*Sub
	for sub := range h.subs {
		if sub.tenant != tenant || !sub.wants(host) {
			continue
		}
		select {
		case sub.ch <- Event{Host: host, Samples: cp}:
		default:
			slow = append(slow, sub)
		}
	}
	for _, sub := range slow {
		h.removeLocked(sub)
		h.slowDropped.Add(1)
	}
	h.mu.Unlock()
}

// Subscribe registers a subscriber for tenant. hosts nil means all of the
// tenant's hosts. Samples newer than since (per host, bounded) are queued
// first as backfill.
func (h *Hub) Subscribe(tenant domain.TenantID, hosts []domain.HostID, since time.Time) *Sub {
	sub := newSub(tenant, hosts)
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, r := range h.rings {
		if k.tenant != tenant || !sub.wants(k.host) || len(sub.ch) >= sendBuffer {
			continue
		}
		if bf := r.since(since, backfillMax); len(bf) > 0 {
			sub.ch <- Event{Host: k.host, Samples: bf}
		}
	}
	h.subs[sub] = struct{}{}
	return sub
}

// Unsubscribe removes a subscriber (idempotent).
func (h *Hub) Unsubscribe(sub *Sub) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.removeLocked(sub)
}

func (h *Hub) removeLocked(sub *Sub) {
	if _, ok := h.subs[sub]; ok {
		delete(h.subs, sub)
		close(sub.ch)
	}
}

// SlowDropped counts subscribers closed for falling behind.
func (h *Hub) SlowDropped() uint64 { return h.slowDropped.Load() }

// Subscribers is the current subscriber count.
func (h *Hub) Subscribers() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}
