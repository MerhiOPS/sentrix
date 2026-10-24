package storetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

func newUser(t *testing.T, st Stores, s domain.Scope, email string, role domain.Role) domain.User {
	t.Helper()
	tid, _ := s.Tenant()
	u := domain.User{ID: domain.NewUserID(), Tenant: tid, Email: email, Role: role, CreatedAt: T0}
	if err := st.CreateUser(context.Background(), s, u, "$argon2id$stub"); err != nil {
		t.Fatal(err)
	}
	return u
}

func newSession(t *testing.T, st Stores, s domain.Scope, u domain.User) []byte {
	t.Helper()
	h := identity.HashSecret(identity.NewSecret())
	if err := st.CreateSession(context.Background(), s, domain.Session{TokenHash: h, User: u.ID, Tenant: u.Tenant,
		CreatedAt: T0, LastSeen: T0}); err != nil {
		t.Fatal(err)
	}
	return h
}

func testUserEmailUniqueAcrossTenants(t *testing.T, st Stores) {
	a, _ := newTenant(t, st, "a")
	b, _ := newTenant(t, st, "b")
	newUser(t, st, a, "ops@example.com", domain.RoleAdmin)
	tid, _ := b.Tenant()
	err := st.CreateUser(context.Background(), b,
		domain.User{ID: domain.NewUserID(), Tenant: tid, Email: "ops@example.com", Role: domain.RoleViewer, CreatedAt: T0}, "x")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("same email in another tenant: err = %v, want ErrConflict", err)
	}
}

func testFindLoginReturnsTenantAndHash(t *testing.T, st Stores) {
	s, tid := newTenant(t, st, "a")
	u := newUser(t, st, s, "ops@example.com", domain.RoleViewer)
	got, hash, err := st.FindLogin(context.Background(), "ops@example.com")
	if err != nil || got.ID != u.ID || got.Tenant != tid || got.Role != domain.RoleViewer || hash != "$argon2id$stub" {
		t.Fatalf("FindLogin = %+v %q %v", got, hash, err)
	}
	if _, _, err := st.FindLogin(context.Background(), "nobody@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown email: err = %v", err)
	}
}

func testSessionLifecycle(t *testing.T, st Stores) {
	ctx := context.Background()
	s, tid := newTenant(t, st, "a")
	u := newUser(t, st, s, "ops@example.com", domain.RoleAdmin)
	h := newSession(t, st, s, u)
	got, err := st.GetSession(ctx, h)
	if err != nil || got.User != u.ID || got.Tenant != tid || got.Role != domain.RoleAdmin || !bytes.Equal(got.TokenHash, h) {
		t.Fatalf("GetSession = %+v, %v", got, err)
	}
	later := T0.Add(5 * time.Minute)
	if err := st.TouchSession(ctx, s, h, later); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetSession(ctx, h); !got.LastSeen.Equal(later) {
		t.Fatalf("LastSeen after touch = %v", got.LastSeen)
	}
	if err := st.DeleteSession(ctx, s, h); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteSession(ctx, s, h); err != nil {
		t.Fatalf("delete is not idempotent: %v", err)
	}
	if _, err := st.GetSession(ctx, h); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted session still resolves: %v", err)
	}
}

// userCrossCases extend crossCases for the ports added in Phase 2.
var userCrossCases = map[string]crossCase{
	"LatestReader.LatestCore": func(ctx context.Context, st Stores, a, b fixture) string {
		got, err := st.LatestCore(ctx, b.scope)
		if err != nil {
			return "LatestCore(B): " + err.Error()
		}
		if _, leaked := got[a.host]; leaked {
			return "LatestCore(B) returned A's host"
		}
		return ""
	},
	"UserRepo.CreateUser": func(ctx context.Context, st Stores, a, b fixture) string {
		u := domain.User{ID: domain.NewUserID(), Tenant: a.tenant, Email: "x-" + domain.NewID().String() + "@e.com", Role: domain.RoleAdmin, CreatedAt: T0}
		if err := st.CreateUser(ctx, b.scope, u, "h"); err != nil {
			return "CreateUser(B): " + err.Error()
		}
		got, _, err := st.FindLogin(ctx, u.Email)
		if err != nil || got.Tenant != b.tenant {
			return fmt.Sprintf("user created under B landed in tenant %v (%v): a Tenant field in the input must not choose the tenant", got.Tenant, err)
		}
		return ""
	},
	"UserRepo.GetUser": func(ctx context.Context, st Stores, a, b fixture) string {
		if _, err := st.GetUser(ctx, b.scope, a.user); !errors.Is(err, domain.ErrNotFound) {
			return fmt.Sprintf("GetUser(B, A's user) err = %v, want ErrNotFound", err)
		}
		return ""
	},
	"UserRepo.ListUsers": func(ctx context.Context, st Stores, a, b fixture) string {
		us, err := st.ListUsers(ctx, b.scope)
		for _, u := range us {
			if u.ID == a.user || u.Tenant != b.tenant {
				return fmt.Sprintf("ListUsers(B) returned user %v of tenant %v", u.ID, u.Tenant)
			}
		}
		if err != nil {
			return "ListUsers(B): " + err.Error()
		}
		return ""
	},
	"SessionRepo.CreateSession": func(ctx context.Context, st Stores, a, b fixture) string {
		h := identity.HashSecret(identity.NewSecret())
		err := st.CreateSession(ctx, b.scope, domain.Session{TokenHash: h, User: a.user, Tenant: a.tenant, CreatedAt: T0, LastSeen: T0})
		if !errors.Is(err, domain.ErrNotFound) {
			return fmt.Sprintf("CreateSession(B) for A's user: err = %v, want ErrNotFound", err)
		}
		return ""
	},
	"SessionRepo.TouchSession": func(ctx context.Context, st Stores, a, b fixture) string {
		_ = st.TouchSession(ctx, b.scope, a.session, T0.Add(time.Hour))
		if got, err := st.GetSession(ctx, a.session); err != nil || !got.LastSeen.Equal(T0) {
			return fmt.Sprintf("TouchSession(B) moved A's session to %v (%v)", got.LastSeen, err)
		}
		return ""
	},
	"SessionRepo.DeleteSession": func(ctx context.Context, st Stores, a, b fixture) string {
		_ = st.DeleteSession(ctx, b.scope, a.session)
		if _, err := st.GetSession(ctx, a.session); err != nil {
			return "DeleteSession(B) deleted A's session: " + err.Error()
		}
		return ""
	},
}
