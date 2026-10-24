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
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Store holds everything behind one mutex. Correctness, not speed.
type Store struct {
	mu       sync.Mutex
	tenants  map[domain.TenantID]domain.Tenant
	tokens   map[string]*token // by secret hash
	hosts    map[domain.HostID]*host
	users    map[domain.UserID]*user
	sessions map[string]*session // by token hash
	limits   map[domain.TenantID]int
	// nextSeries numbers series across the store, like an INTEGER PRIMARY KEY.
	nextSeries int64
	nextLog    int64
	nextRule   int64
	logLimits  map[domain.TenantID]int64
	logUsage   map[usageKey]int64
	rules      map[domain.TenantID][]domain.RedactionRule
	alerts     alertData
	now        func() time.Time
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
	// rollup1m and rollup1h are what the rollup tables hold: copies of the
	// samples with their own retention, so pruning raw keeps the history.
	rollup1m map[int64]domain.CoreSample
	rollup1h map[int64]domain.CoreSample
	latest   *domain.CoreSample
	series   map[string]*mseries // by metric|labels hash
	snaps    map[int64]domain.ProcessSnapshot
	logs     []logRow
}

// New returns an empty store.
func New() *Store {
	return &Store{
		tenants:   map[domain.TenantID]domain.Tenant{},
		tokens:    map[string]*token{},
		hosts:     map[domain.HostID]*host{},
		users:     map[domain.UserID]*user{},
		sessions:  map[string]*session{},
		limits:    map[domain.TenantID]int{},
		logLimits: map[domain.TenantID]int64{},
		logUsage:  map[usageKey]int64{},
		rules:     map[domain.TenantID][]domain.RedactionRule{},
		alerts:    newAlertData(),
		now:       time.Now,
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

// SetSeriesLimit implements identity.TenantRepo.
func (s *Store) SetSeriesLimit(_ context.Context, ops domain.OpsScope, t domain.TenantID, limit int) error {
	if _, err := ops.Reason(); err != nil {
		return err
	}
	if limit < 0 {
		return fmt.Errorf("%w: series limit must be >= 0", domain.ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tenants[t]; !ok {
		return fmt.Errorf("tenant: %w", domain.ErrNotFound)
	}
	s.limits[t] = limit
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
