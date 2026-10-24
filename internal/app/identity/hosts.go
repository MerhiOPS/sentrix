package identity

import (
	"context"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// HostRepo changes a host's lifecycle. Another tenant's host is
// domain.ErrNotFound.
type HostRepo interface {
	// SetHostDisabled sets disabled_at (nil re-enables).
	SetHostDisabled(ctx context.Context, s domain.Scope, id domain.HostID, at *time.Time) error
}

// CredentialCache forgets cached agent credentials of a host.
type CredentialCache interface {
	Invalidate(host domain.HostID)
}

// Hosts is the host lifecycle use case. A disabled host's agent is refused
// at once (its cached credential is dropped, not left to expire) and the
// host is no longer evaluated by alerting (OLD C-M2).
type Hosts struct {
	repo  HostRepo
	cache CredentialCache
	now   func() time.Time
}

// NewHosts builds the use case.
func NewHosts(repo HostRepo, cache CredentialCache, now func() time.Time) *Hosts {
	return &Hosts{repo: repo, cache: cache, now: now}
}

// Disable stops a host ingesting and being evaluated.
func (h *Hosts) Disable(ctx context.Context, s domain.Scope, id domain.HostID) error {
	at := h.now().UTC()
	if err := h.repo.SetHostDisabled(ctx, s, id, &at); err != nil {
		return err
	}
	h.cache.Invalidate(id)
	return nil
}

// Enable lets a disabled host ingest again.
func (h *Hosts) Enable(ctx context.Context, s domain.Scope, id domain.HostID) error {
	return h.repo.SetHostDisabled(ctx, s, id, nil)
}
