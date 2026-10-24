package storetest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// BenchHost is a seeded host for adapter benchmarks.
type BenchHost struct {
	Scope domain.Scope
	Host  domain.HostID
}

// SeedBenchHosts creates one tenant with n enrolled hosts, so a benchmark's
// searches run over every line it stored.
func SeedBenchHosts(tb testing.TB, st Stores, n int) []BenchHost {
	tb.Helper()
	ctx := context.Background()
	tid := domain.NewTenantID()
	ops, _ := domain.NewOpsScope("benchmark")
	if err := st.CreateTenant(ctx, ops, domain.Tenant{ID: tid, Name: "bench", CreatedAt: time.Now()}); err != nil {
		tb.Fatal(err)
	}
	if err := st.SetLogLimit(ctx, ops, tid, 1<<40); err != nil {
		tb.Fatal(err)
	}
	sc, _ := domain.NewScope(tid)
	out := make([]BenchHost, 0, n)
	for i := 0; i < n; i++ {
		secret := identity.NewSecret()
		tok := identity.EnrollmentToken{SecretHash: identity.HashSecret(secret), CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
		if err := st.CreateToken(ctx, sc, tok); err != nil {
			tb.Fatal(err)
		}
		h, err := st.RedeemToken(ctx, tok.SecretHash, time.Now(), identity.NewHost{
			Host:           domain.Host{ID: domain.NewHostID(), Name: fmt.Sprintf("bench-%d", i), OS: "linux", Interval: 5 * time.Second, CreatedAt: time.Now()},
			CredentialHash: identity.HashSecret(identity.NewSecret()),
		})
		if err != nil {
			tb.Fatal(err)
		}
		out = append(out, BenchHost{Scope: sc, Host: h.ID})
	}
	return out
}
