package livehub

import (
	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// LogFilter is a live tail's filter. It is applied in the hub, with the same
// semantics as search, so the browser receives only what it asked for (OLD
// B10: tail filters that were silently ignored).
type LogFilter struct {
	Hosts   []domain.HostID // nil: all of the tenant's hosts; empty: none
	Levels  []domain.Level  // empty: all levels
	Clauses []query.Clause
}

// LogEvent is one host's newly stored, redacted lines that matched.
type LogEvent struct {
	Host  domain.HostID
	Lines []domain.LogLine
}

// LogSub is a live-tail subscriber. Its channel closes when it is dropped.
type LogSub struct {
	tenant  domain.TenantID
	hosts   map[domain.HostID]bool // nil: every host
	levels  map[domain.Level]bool
	clauses []query.Clause
	ch      chan LogEvent
}

func (s *LogSub) match(host domain.HostID, l domain.LogLine) bool {
	if s.hosts != nil && !s.hosts[host] {
		return false
	}
	if len(s.levels) > 0 && !s.levels[l.Level] {
		return false
	}
	return query.Matches(s.clauses, l.Message)
}

// Events is the subscriber's feed.
func (s *LogSub) Events() <-chan LogEvent { return s.ch }

// SubscribeLogs registers a live tail for tenant.
func (h *Hub) SubscribeLogs(tenant domain.TenantID, f LogFilter) *LogSub {
	sub := &LogSub{tenant: tenant, clauses: f.Clauses, ch: make(chan LogEvent, sendBuffer), levels: map[domain.Level]bool{}}
	if f.Hosts != nil { // same meaning as the metrics feed: an explicit list, possibly empty
		sub.hosts = make(map[domain.HostID]bool, len(f.Hosts))
		for _, id := range f.Hosts {
			sub.hosts[id] = true
		}
	}
	for _, l := range f.Levels {
		sub.levels[l] = true
	}
	h.mu.Lock()
	h.logSubs[sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

// UnsubscribeLogs removes a live tail (idempotent).
func (h *Hub) UnsubscribeLogs(sub *LogSub) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.removeLogLocked(sub)
}

func (h *Hub) removeLogLocked(sub *LogSub) {
	if _, ok := h.logSubs[sub]; ok {
		delete(h.logSubs, sub)
		close(sub.ch)
	}
}

// PublishLogs implements ingest.Publisher. Never blocks: a slow tail is
// closed and counted.
func (h *Hub) PublishLogs(tenant domain.TenantID, host domain.HostID, lines []domain.LogLine) {
	if len(lines) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var slow []*LogSub
	for sub := range h.logSubs {
		if sub.tenant != tenant {
			continue
		}
		var matched []domain.LogLine
		for _, l := range lines {
			if sub.match(host, l) {
				matched = append(matched, l)
			}
		}
		if len(matched) == 0 {
			continue
		}
		select {
		case sub.ch <- LogEvent{Host: host, Lines: matched}:
		default:
			slow = append(slow, sub)
		}
	}
	for _, sub := range slow {
		h.removeLogLocked(sub)
		h.slowDropped.Add(1)
	}
}
