// Package memstore is the in-memory reference implementation of the storage
// ports. It exists so the contract suite runs against two independent
// implementations (PLAN §1): one adapter passing a suite proves nothing about
// whether the suite describes the ports or just that adapter.
package memstore

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Store holds everything behind one mutex. Correctness, not speed.
type Store struct {
	mu      sync.Mutex
	tenants map[domain.TenantID]domain.Tenant
	tokens  map[string]*token // by secret hash
	hosts   map[domain.HostID]*host
}

type token struct {
	tenant    domain.TenantID
	expiresAt int64
	used      bool
}

type host struct {
	h        domain.Host
	credHash []byte
	seqs     map[uint64]int64 // seq → received unix ms
	counters ingest.CounterState
	samples  map[int64]domain.CoreSample // by ts unix ms
	latest   *domain.CoreSample
}

// New returns an empty store.
func New() *Store {
	return &Store{
		tenants: map[domain.TenantID]domain.Tenant{},
		tokens:  map[string]*token{},
		hosts:   map[domain.HostID]*host{},
	}
}

// CreateTenant implements identity.TenantRepo.
func (s *Store) CreateTenant(_ context.Context, ops domain.OpsScope, t domain.Tenant) error {
	if _, err := ops.Reason(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.tenants[t.ID]; dup {
		return fmt.Errorf("tenant %s: %w", t.ID, domain.ErrConflict)
	}
	s.tenants[t.ID] = t
	return nil
}

// GetTenant implements identity.TenantRepo.
func (s *Store) GetTenant(_ context.Context, sc domain.Scope) (domain.Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return domain.Tenant{}, err
	}
	return s.tenants[tid], nil
}

// tenantOf resolves a scope to an existing tenant. Caller holds mu.
func (s *Store) tenantOf(sc domain.Scope) (domain.TenantID, error) {
	tid, err := sc.Tenant()
	if err != nil {
		return domain.TenantID{}, err
	}
	if _, ok := s.tenants[tid]; !ok {
		return domain.TenantID{}, fmt.Errorf("tenant: %w", domain.ErrNotFound)
	}
	return tid, nil
}

// hostIn returns the host only if it belongs to the scope's tenant. Caller
// holds mu. Another tenant's host is ErrNotFound — the isolation rule.
func (s *Store) hostIn(sc domain.Scope, id domain.HostID) (*host, error) {
	tid, err := s.tenantOf(sc)
	if err != nil {
		return nil, err
	}
	h, ok := s.hosts[id]
	if !ok || h.h.Tenant != tid {
		return nil, fmt.Errorf("host: %w", domain.ErrNotFound)
	}
	return h, nil
}

// AuthenticateCredential implements ingest.CredentialStore.
func (s *Store) AuthenticateCredential(_ context.Context, credHash []byte) (ingest.AuthHost, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, h := range s.hosts {
		if bytes.Equal(h.credHash, credHash) {
			return ingest.AuthHost{Tenant: h.h.Tenant, Host: h.h.ID, Interval: h.h.Interval, Enabled: h.h.Enabled()}, nil
		}
	}
	return ingest.AuthHost{}, fmt.Errorf("credential: %w", domain.ErrNotFound)
}
