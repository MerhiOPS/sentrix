package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

type hostRepo struct {
	at  *time.Time
	err error
}

func (r *hostRepo) SetHostDisabled(_ context.Context, _ domain.Scope, _ domain.HostID, at *time.Time) error {
	if r.err != nil {
		return r.err
	}
	r.at = at
	return nil
}

type cache struct{ dropped []domain.HostID }

func (c *cache) Invalidate(h domain.HostID) { c.dropped = append(c.dropped, h) }

func TestDisableDropsCachedCredential(t *testing.T) {
	repo, c := &hostRepo{}, &cache{}
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	hs := NewHosts(repo, c, func() time.Time { return now })
	id := domain.NewHostID()
	if err := hs.Disable(context.Background(), domain.Scope{}, id); err != nil {
		t.Fatal(err)
	}
	if repo.at == nil || !repo.at.Equal(now) || len(c.dropped) != 1 || c.dropped[0] != id {
		t.Fatalf("disable: stored %v, dropped %v", repo.at, c.dropped)
	}
	if err := hs.Enable(context.Background(), domain.Scope{}, id); err != nil || repo.at != nil {
		t.Fatalf("enable: %v, stored %v", err, repo.at)
	}
	repo.err = domain.ErrNotFound
	c.dropped = nil
	if err := hs.Disable(context.Background(), domain.Scope{}, id); !errors.Is(err, domain.ErrNotFound) || len(c.dropped) != 0 {
		t.Fatalf("a failed disable: %v, dropped %v", err, c.dropped)
	}
}
