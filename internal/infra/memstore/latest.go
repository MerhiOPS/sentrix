package memstore

import (
	"context"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// LatestCore implements query.LatestReader.
func (s *Store) LatestCore(_ context.Context, sc domain.Scope) (map[domain.HostID]domain.CoreSample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return nil, err
	}
	out := map[domain.HostID]domain.CoreSample{}
	for id, h := range s.hosts {
		if h.h.Tenant == tid && h.latest != nil {
			out[id] = roundGauges(*h.latest)
		}
	}
	return out, nil
}
