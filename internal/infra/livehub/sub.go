package livehub

import "github.com/MerhiOPS/sentrix/internal/domain"

// Sub is one subscriber. Its channel is closed when it is unsubscribed or
// dropped for being slow; the reader treats a closed channel as "reconnect".
type Sub struct {
	tenant domain.TenantID
	hosts  map[domain.HostID]bool // nil: every host of the tenant
	ch     chan Event
}

func newSub(tenant domain.TenantID, hosts []domain.HostID) *Sub {
	s := &Sub{tenant: tenant, ch: make(chan Event, sendBuffer)}
	if hosts != nil {
		s.hosts = make(map[domain.HostID]bool, len(hosts))
		for _, h := range hosts {
			s.hosts[h] = true
		}
	}
	return s
}

func (s *Sub) wants(h domain.HostID) bool { return s.hosts == nil || s.hosts[h] }

// Events is the subscriber's feed.
func (s *Sub) Events() <-chan Event { return s.ch }
