package storetest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// testMaxHostsRefusesOnlyNewEnrollments: Phase 7 acceptance. Over the
// limit a new enrollment is refused (and its token stays usable), while the
// existing host keeps reporting. Disabling a host frees a slot.
func testMaxHostsRefusesOnlyNewEnrollments(t *testing.T, st Stores) {
	ctx := context.Background()
	f := newFixture(t, st, "a")
	plan := domain.DefaultPlan
	plan.MaxHosts = 1
	if err := st.SetPlan(ctx, mustOps(t), f.tenant, plan); err != nil {
		t.Fatal(err)
	}
	tok := identity.EnrollmentToken{SecretHash: identity.HashSecret(identity.NewSecret()), CreatedAt: T0, ExpiresAt: T0.Add(time.Hour)}
	if err := st.CreateToken(ctx, f.scope, tok); err != nil {
		t.Fatal(err)
	}
	nh := identity.NewHost{Host: domain.Host{ID: domain.NewHostID(), Name: "second", OS: "linux", Interval: 5 * time.Second, CreatedAt: T0},
		CredentialHash: identity.HashSecret(identity.NewSecret())}
	if _, err := st.RedeemToken(ctx, tok.SecretHash, T0, nh); !errors.Is(err, domain.ErrQuotaExceeded) {
		t.Fatalf("enrollment over max_hosts: %v, want ErrQuotaExceeded", err)
	}
	if _, err := st.CommitBatch(ctx, f.scope, f.host, 1, prepared(T0, []int{0}, []uint64{1})); err != nil {
		t.Fatalf("the existing host stopped reporting over the limit: %v", err)
	}
	at := T0
	if err := st.SetHostDisabled(ctx, f.scope, f.host, &at); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RedeemToken(ctx, tok.SecretHash, T0, nh); err != nil {
		t.Fatalf("after disabling a host the refused token should work: %v", err)
	}
	if got, _ := st.GetPlan(ctx, f.scope); got != plan {
		t.Fatalf("plan = %+v, want %+v", got, plan)
	}
}

// testMeteringFromWritePath: samples, log bytes and the daily host peak are
// counted at commit; a replayed batch adds nothing.
func testMeteringFromWritePath(t *testing.T, st Stores) {
	ctx := context.Background()
	f := newFixture(t, st, "a")
	second, _ := enroll(t, st, f.scope, "second")
	if _, err := st.CommitBatch(ctx, f.scope, f.host, 1, prepared(T0, []int{0, 1, 2}, []uint64{1, 2, 3})); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CommitBatch(ctx, f.scope, f.host, 1, prepared(T0, []int{3, 4}, []uint64{4, 5})); err != nil {
		t.Fatal(err) // a replay of seq 1: acknowledged, applied and metered nothing
	}
	if _, err := st.CommitBatch(ctx, f.scope, f.host, 2, withLogs(nil, line(0, domain.LevelInfo, "metered-line"))); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CommitBatch(ctx, f.scope, second.ID, 3, prepared(T0, []int{0}, []uint64{1})); err != nil {
		t.Fatal(err)
	}
	u, err := st.GetUsage(ctx, f.scope, T0)
	if err != nil {
		t.Fatal(err)
	}
	if u.MonthSamples != 4 || u.MonthPeakHosts != 2 || u.Hosts != 2 || u.MonthLogBytes <= 0 || u.LogBytesToday != u.MonthLogBytes {
		t.Fatalf("usage %+v: want 4 samples (the replay not counted), 2 hosts, the log bytes today", u)
	}
	if next, _ := st.GetUsage(ctx, f.scope, T0.AddDate(0, 1, 0)); next.MonthSamples != 0 || next.LogBytesToday != 0 {
		t.Fatalf("next month's usage %+v, want zero", next)
	}
}

// planCrossCases cover PlanRepo.
var planCrossCases = map[string]crossCase{
	"PlanRepo.GetPlan": func(ctx context.Context, st Stores, a, b fixture) string {
		p := domain.DefaultPlan
		p.MaxHosts = 7
		ops, _ := domain.NewOpsScope("storetest")
		if err := st.SetPlan(ctx, ops, a.tenant, p); err != nil {
			return err.Error()
		}
		if got, err := st.GetPlan(ctx, b.scope); err != nil || got.MaxHosts == 7 {
			return fmt.Sprintf("GetPlan(B) = %+v, %v: got A's plan", got, err)
		}
		return ""
	},
	"PlanRepo.GetUsage": func(ctx context.Context, st Stores, a, b fixture) string {
		before, _ := st.GetUsage(ctx, b.scope, T0)
		if _, err := st.CommitBatch(ctx, a.scope, a.host, 701, prepared(T0, []int{100, 101}, []uint64{1, 2})); err != nil {
			return err.Error()
		}
		after, err := st.GetUsage(ctx, b.scope, T0)
		if err != nil || after != before {
			return fmt.Sprintf("GetUsage(B) changed with A's batch: %+v → %+v (%v)", before, after, err)
		}
		return ""
	},
}
