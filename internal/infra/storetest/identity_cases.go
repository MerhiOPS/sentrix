package storetest

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

func testTenantCreateGet(t *testing.T, st Stores) {
	s, tid := newTenant(t, st, "Acme")
	got, err := st.GetTenant(context.Background(), s)
	if err != nil || got.ID != tid || got.Name != "Acme" || !got.CreatedAt.Equal(T0) {
		t.Fatalf("GetTenant = %+v, %v", got, err)
	}
}

func testTenantDuplicate(t *testing.T, st Stores) {
	tn := domain.Tenant{ID: domain.NewTenantID(), Name: "x", CreatedAt: T0}
	ops := mustOps(t)
	if err := st.CreateTenant(context.Background(), ops, tn); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateTenant(context.Background(), ops, tn); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate tenant: err = %v, want ErrConflict", err)
	}
	if err := st.CreateTenant(context.Background(), domain.OpsScope{}, domain.Tenant{ID: domain.NewTenantID()}); err == nil {
		t.Fatal("zero OpsScope accepted")
	}
}

// testUnknownScope: a well-formed scope for a tenant that does not exist
// reads as not found everywhere, and the zero scope is refused outright.
func testUnknownScope(t *testing.T, st Stores) {
	ctx := context.Background()
	ghost, _ := domain.NewScope(domain.NewTenantID())
	if _, err := st.GetTenant(ctx, ghost); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetTenant(unknown) err = %v", err)
	}
	if _, err := st.ListHosts(ctx, ghost); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("ListHosts(unknown) err = %v", err)
	}
	if _, err := st.ListHosts(ctx, domain.Scope{}); err == nil {
		t.Error("ListHosts accepted the zero Scope")
	}
}

func testTokenSingleUse(t *testing.T, st Stores) {
	ctx := context.Background()
	s, _ := newTenant(t, st, "t")
	secret := identity.NewSecret()
	tok := identity.EnrollmentToken{SecretHash: identity.HashSecret(secret), CreatedAt: T0, ExpiresAt: T0.Add(time.Hour)}
	if err := st.CreateToken(ctx, s, tok); err != nil {
		t.Fatal(err)
	}
	nh := func() identity.NewHost {
		return identity.NewHost{Host: domain.Host{ID: domain.NewHostID(), Name: "h", OS: "linux",
			Interval: 5 * time.Second, CreatedAt: T0}, CredentialHash: identity.HashSecret(identity.NewSecret())}
	}
	if _, err := st.RedeemToken(ctx, tok.SecretHash, T0, nh()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RedeemToken(ctx, tok.SecretHash, T0, nh()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("token reuse: err = %v, want ErrNotFound", err)
	}
	hosts, err := st.ListHosts(ctx, s)
	if err != nil || len(hosts) != 1 {
		t.Fatalf("reuse created a second host: %d hosts, %v", len(hosts), err)
	}
}

func testTokenExpired(t *testing.T, st Stores) {
	ctx := context.Background()
	s, _ := newTenant(t, st, "t")
	tok := identity.EnrollmentToken{SecretHash: identity.HashSecret(identity.NewSecret()), CreatedAt: T0, ExpiresAt: T0.Add(time.Hour)}
	if err := st.CreateToken(ctx, s, tok); err != nil {
		t.Fatal(err)
	}
	nh := identity.NewHost{Host: domain.Host{ID: domain.NewHostID(), Name: "h", OS: "linux", Interval: 5 * time.Second, CreatedAt: T0},
		CredentialHash: identity.HashSecret(identity.NewSecret())}
	if _, err := st.RedeemToken(ctx, tok.SecretHash, T0.Add(time.Hour), nh); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("token at expiry: err = %v, want ErrNotFound", err)
	}
}

func testCredentialResolves(t *testing.T, st Stores) {
	ctx := context.Background()
	f := newFixture(t, st, "a")
	ah, err := st.AuthenticateCredential(ctx, f.credHash)
	if err != nil || ah.Tenant != f.tenant || ah.Host != f.host || !ah.Enabled || ah.Interval != 5*time.Second {
		t.Fatalf("AuthenticateCredential = %+v, %v", ah, err)
	}
	wrong := bytes.Repeat([]byte{7}, len(f.credHash))
	if _, err := st.AuthenticateCredential(ctx, wrong); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown credential: err = %v, want ErrNotFound", err)
	}
	h, err := st.GetHost(ctx, f.scope, f.host)
	if err != nil || h.Tenant != f.tenant || h.Name != "a-host" {
		t.Fatalf("GetHost after enroll = %+v, %v", h, err)
	}
}
