package identity

import (
	"context"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Tenants creates tenants (the `sentrix-server tenant create` CLI).
type Tenants struct {
	repo TenantRepo
	now  func() time.Time
}

// NewTenants builds the tenant use cases. now is injectable for tests.
func NewTenants(repo TenantRepo, now func() time.Time) *Tenants {
	return &Tenants{repo: repo, now: now}
}

// Create makes a tenant and returns it.
func (t *Tenants) Create(ctx context.Context, name string) (domain.Tenant, error) {
	n, err := domain.NormalizeTenantName(name)
	if err != nil {
		return domain.Tenant{}, err
	}
	ops, err := domain.NewOpsScope("create tenant")
	if err != nil {
		return domain.Tenant{}, err
	}
	tenant := domain.Tenant{ID: domain.NewTenantID(), Name: n, CreatedAt: t.now().UTC()}
	if err := t.repo.CreateTenant(ctx, ops, tenant); err != nil {
		return domain.Tenant{}, fmt.Errorf("create tenant: %w", err)
	}
	return tenant, nil
}
