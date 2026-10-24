package memstore

import (
	"context"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/storetest"
)

func TestContract(t *testing.T) {
	storetest.Run(t, func(*testing.T) storetest.Stores { return New() })
}

// blind is a deliberately tenant-blind adapter: its reads ignore the scope's
// tenant. The contract must catch it, or the contract proves nothing (R2).
type blind struct{ *Store }

func (b blind) GetHost(ctx context.Context, _ domain.Scope, id domain.HostID) (domain.Host, error) {
	b.mu.Lock()
	h, ok := b.hosts[id]
	b.mu.Unlock()
	if !ok {
		return domain.Host{}, domain.ErrNotFound
	}
	return b.Store.GetHost(ctx, mustScope(h.h.Tenant), id)
}

func (b blind) RawCore(ctx context.Context, _ domain.Scope, id domain.HostID, from, to time.Time) ([]domain.CoreSample, error) {
	b.mu.Lock()
	h, ok := b.hosts[id]
	b.mu.Unlock()
	if !ok {
		return nil, domain.ErrNotFound
	}
	return b.Store.RawCore(ctx, mustScope(h.h.Tenant), id, from, to)
}

func mustScope(t domain.TenantID) domain.Scope {
	s, _ := domain.NewScope(t)
	return s
}

func TestContractCatchesTenantBlindAdapter(t *testing.T) {
	problems := storetest.CrossTenantProblems(context.Background(), t, blind{New()})
	if len(problems) < 2 {
		t.Fatalf("tenant-blind GetHost and RawCore went undetected; problems: %v", problems)
	}
	for _, p := range problems {
		t.Log("caught:", p)
	}
}
