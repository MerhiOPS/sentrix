package storetest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// scopedPorts are the interfaces whose Scope-taking methods must each have a
// cross-tenant case. Adding a port to Stores without adding it here, or a
// method without a case, fails the suite (PLAN §4.1 item 4).
var scopedPorts = []reflect.Type{
	reflect.TypeFor[identity.TenantRepo](),
	reflect.TypeFor[identity.EnrollmentRepo](),
	reflect.TypeFor[identity.UserRepo](),
	reflect.TypeFor[identity.SessionRepo](),
	reflect.TypeFor[ingest.Store](),
	reflect.TypeFor[query.HostReader](),
	reflect.TypeFor[query.CoreReader](),
	reflect.TypeFor[query.LatestReader](),
}

// crossCase calls one method as tenant B against tenant A's data and returns
// a problem description, or "" if B saw nothing of A.
type crossCase func(ctx context.Context, st Stores, a, b fixture) string

var crossCases = map[string]crossCase{
	"TenantRepo.GetTenant": func(ctx context.Context, st Stores, a, b fixture) string {
		got, err := st.GetTenant(ctx, b.scope)
		if err != nil || got.ID != b.tenant {
			return fmt.Sprintf("GetTenant(B) = %v, %v; want B", got.ID, err)
		}
		return ""
	},
	"EnrollmentRepo.CreateToken": func(ctx context.Context, st Stores, a, b fixture) string {
		tok := identity.EnrollmentToken{SecretHash: identity.HashSecret(identity.NewSecret()), CreatedAt: T0, ExpiresAt: T0.Add(time.Hour)}
		if err := st.CreateToken(ctx, b.scope, tok); err != nil {
			return "CreateToken(B): " + err.Error()
		}
		h, err := st.RedeemToken(ctx, tok.SecretHash, T0, identity.NewHost{
			Host:           domain.Host{ID: domain.NewHostID(), Name: "x", OS: "linux", Interval: 5 * time.Second, CreatedAt: T0},
			CredentialHash: identity.HashSecret(identity.NewSecret()),
		})
		if err != nil || h.Tenant != b.tenant {
			return fmt.Sprintf("token issued under B redeemed into tenant %v (%v)", h.Tenant, err)
		}
		return ""
	},
	"Store.CommitBatch": func(ctx context.Context, st Stores, a, b fixture) string {
		before := countSamples(ctx, st, a)
		_, err := st.CommitBatch(ctx, b.scope, a.host, 999, prepared(T0, []int{50}, []uint64{1}))
		if !errors.Is(err, domain.ErrNotFound) {
			return fmt.Sprintf("CommitBatch(B, A's host) err = %v, want ErrNotFound", err)
		}
		if after := countSamples(ctx, st, a); after != before {
			return fmt.Sprintf("CommitBatch(B) wrote into A's host: %d → %d samples", before, after)
		}
		return ""
	},
	"HostReader.GetHost": func(ctx context.Context, st Stores, a, b fixture) string {
		if h, err := st.GetHost(ctx, b.scope, a.host); !errors.Is(err, domain.ErrNotFound) {
			return fmt.Sprintf("GetHost(B, A's host) = %v, %v; want ErrNotFound", h.ID, err)
		}
		return ""
	},
	"HostReader.ListHosts": func(ctx context.Context, st Stores, a, b fixture) string {
		hs, err := st.ListHosts(ctx, b.scope)
		if err != nil {
			return "ListHosts(B): " + err.Error()
		}
		for _, h := range hs {
			if h.ID == a.host || h.Tenant != b.tenant {
				return fmt.Sprintf("ListHosts(B) returned host %v of tenant %v", h.ID, h.Tenant)
			}
		}
		return ""
	},
	"CoreReader.RawCore": func(ctx context.Context, st Stores, a, b fixture) string {
		ss, err := st.RawCore(ctx, b.scope, a.host, T0.Add(-time.Hour), T0.Add(time.Hour))
		if len(ss) > 0 || !errors.Is(err, domain.ErrNotFound) {
			return fmt.Sprintf("RawCore(B, A's host) = %d samples, %v; want ErrNotFound", len(ss), err)
		}
		return ""
	},
}

func countSamples(ctx context.Context, st Stores, f fixture) int {
	ss, _ := st.RawCore(ctx, f.scope, f.host, T0.Add(-time.Hour), T0.Add(24*time.Hour))
	return len(ss)
}

// CrossTenantProblems seeds two tenants with identical fixtures, runs every
// cross-tenant case, and returns what leaked. It returns problems instead of
// failing, so a test can prove it catches a tenant-blind adapter (R2).
func CrossTenantProblems(ctx context.Context, t *testing.T, st Stores) []string {
	a, b := newFixture(t, st, "a"), newFixture(t, st, "b")
	for _, f := range []fixture{a, b} {
		if _, err := st.CommitBatch(ctx, f.scope, f.host, 1, prepared(T0, []int{0, 1}, []uint64{1, 2})); err != nil {
			return []string{"seed: " + err.Error()}
		}
	}
	var problems []string
	for _, name := range scopedMethods() {
		c, ok := crossCases[name]
		if !ok {
			c, ok = userCrossCases[name]
		}
		if !ok {
			problems = append(problems, "no cross-tenant case for scoped method "+name)
			continue
		}
		if p := c(ctx, st, a, b); p != "" {
			problems = append(problems, name+": "+p)
		}
	}
	return problems
}

// scopedMethods lists "Port.Method" for every port method with a Scope
// parameter.
func scopedMethods() []string {
	scopeT := reflect.TypeFor[domain.Scope]()
	var out []string
	for _, port := range scopedPorts {
		for i := 0; i < port.NumMethod(); i++ {
			m := port.Method(i)
			for j := 0; j < m.Type.NumIn(); j++ {
				if m.Type.In(j) == scopeT {
					out = append(out, port.Name()+"."+m.Name)
					break
				}
			}
		}
	}
	sort.Strings(out)
	return out
}
