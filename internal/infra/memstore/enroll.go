package memstore

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// CreateToken implements identity.EnrollmentRepo.
func (s *Store) CreateToken(_ context.Context, sc domain.Scope, t identity.EnrollmentToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return err
	}
	key := string(t.SecretHash)
	if _, dup := s.tokens[key]; dup {
		return fmt.Errorf("token: %w", domain.ErrConflict)
	}
	s.tokens[key] = &token{tenant: tid, expiresAt: t.ExpiresAt.UnixMilli()}
	return nil
}

// RedeemToken implements identity.EnrollmentRepo.
func (s *Store) RedeemToken(_ context.Context, secretHash []byte, now time.Time, nh identity.NewHost) (domain.Host, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tokens[string(secretHash)]
	if !ok || t.used || t.expiresAt <= now.UnixMilli() {
		return domain.Host{}, fmt.Errorf("enrollment token: %w", domain.ErrNotFound)
	}
	if _, dup := s.hosts[nh.Host.ID]; dup {
		return domain.Host{}, fmt.Errorf("host %s: %w", nh.Host.ID, domain.ErrConflict)
	}
	for _, h := range s.hosts {
		if bytes.Equal(h.credHash, nh.CredentialHash) {
			return domain.Host{}, fmt.Errorf("credential: %w", domain.ErrConflict)
		}
	}
	if s.enabledHosts(t.tenant) >= s.planOf(t.tenant).MaxHosts {
		return domain.Host{}, fmt.Errorf("%w: the plan's host limit is reached", domain.ErrQuotaExceeded)
	}
	t.used = true
	h := nh.Host
	h.Tenant = t.tenant
	s.hosts[h.ID] = &host{
		h: h, credHash: append([]byte(nil), nh.CredentialHash...),
		seqs: map[uint64]int64{}, counters: ingest.CounterState{}, samples: map[int64]domain.CoreSample{},
		rollup1m: map[int64]domain.CoreSample{}, rollup1h: map[int64]domain.CoreSample{},
		series: map[string]*mseries{}, snaps: map[int64]domain.ProcessSnapshot{},
	}
	return h, nil
}
