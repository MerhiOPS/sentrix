package domain

import (
	"errors"
	"fmt"
)

// Scope is the only way into tenant-owned data. Its field is unexported, so
// the zero Scope is the only one buildable without NewScope — and every
// adapter rejects it via Tenant. A tenant-less query cannot be expressed.
type Scope struct {
	tenant TenantID
}

// NewScope builds a scope for one tenant. Handlers call it with a tenant
// taken from a session or an agent credential, never from request input.
func NewScope(t TenantID) (Scope, error) {
	if t.IsZero() {
		return Scope{}, fmt.Errorf("%w: scope requires a tenant", ErrInvalid)
	}
	return Scope{tenant: t}, nil
}

// errZeroScope is returned by Tenant on an unbuilt Scope.
var errZeroScope = errors.New("zero scope: built without NewScope")

// Tenant returns the scoped tenant, or an error for the zero Scope. Adapters
// call this first in every scoped method, so a forgotten NewScope fails
// loudly instead of matching tenant_id = zero.
func (s Scope) Tenant() (TenantID, error) {
	if s.tenant.IsZero() {
		return TenantID{}, errZeroScope
	}
	return s.tenant, nil
}

// OpsScope authorises cross-tenant work: the retention janitor, metering,
// backup, erasure. It is a distinct type so cross-tenant access is visible in
// signatures and trivially greppable, and it records why it was taken.
type OpsScope struct {
	reason string
}

// NewOpsScope builds an operator scope. reason appears in logs and audit
// entries; an empty reason is refused so every use explains itself.
func NewOpsScope(reason string) (OpsScope, error) {
	if reason == "" {
		return OpsScope{}, fmt.Errorf("%w: ops scope requires a reason", ErrInvalid)
	}
	return OpsScope{reason: reason}, nil
}

// Reason returns why the scope was taken, or an error for the zero OpsScope.
func (o OpsScope) Reason() (string, error) {
	if o.reason == "" {
		return "", errors.New("zero ops scope: built without NewOpsScope")
	}
	return o.reason, nil
}
