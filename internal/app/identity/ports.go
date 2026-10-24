// Package identity holds the identity bounded context: tenants, enrollment
// tokens and host credentials (users and sessions arrive in Phase 2).
package identity

import (
	"context"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// TenantRepo stores tenants. Creating one is cross-tenant by nature, so it
// takes an OpsScope.
type TenantRepo interface {
	CreateTenant(ctx context.Context, ops domain.OpsScope, t domain.Tenant) error
	GetTenant(ctx context.Context, s domain.Scope) (domain.Tenant, error)
	// SetSeriesLimit sets the tenant's detail-series ceiling (plans, Phase 7).
	SetSeriesLimit(ctx context.Context, ops domain.OpsScope, t domain.TenantID, limit int) error
}

// EnrollmentToken is a stored token: only the hash of its secret is kept.
type EnrollmentToken struct {
	SecretHash []byte
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

// NewHost is what redeeming a token creates.
type NewHost struct {
	Host           domain.Host // Tenant is ignored: it comes from the token
	CredentialHash []byte
}

// EnrollmentRepo stores enrollment tokens and redeems them.
type EnrollmentRepo interface {
	CreateToken(ctx context.Context, s domain.Scope, t EnrollmentToken) error

	// RedeemToken atomically marks the token used and creates the host with
	// its credential, under the token's tenant. It is deliberately unscoped:
	// the agent has no tenant yet — the token *is* how the tenant is found.
	// An unknown, expired or already-used token is domain.ErrNotFound; the
	// three are not distinguished to a caller.
	RedeemToken(ctx context.Context, secretHash []byte, now time.Time, nh NewHost) (domain.Host, error)
}
