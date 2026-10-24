package storetest

import (
	"context"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// alertStateCrossCases cover StateRepo, BucketReader and HostRepo.
var alertStateCrossCases = map[string]crossCase{
	"StateRepo.AlertStates": func(ctx context.Context, st Stores, a, b fixture) string {
		if _, _, _, err := seedAlerting(ctx, st, a); err != nil {
			return err.Error()
		}
		got, err := st.AlertStates(ctx, b.scope)
		for _, s := range got {
			if s.Host == a.host {
				return "AlertStates(B) returned A's state"
			}
		}
		if err != nil {
			return err.Error()
		}
		return ""
	},
	"StateRepo.RecordTransition": func(ctx context.Context, st Stores, a, b fixture) string {
		rule, _, _, err := seedAlerting(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		mine, _ := st.CreateRule(ctx, b.scope, alertRule(nil, nil))
		ev := &domain.AlertEvent{From: domain.StateFiring, To: domain.StateOK, At: T0.Add(time.Minute)}
		for _, c := range []struct {
			name string
			st   domain.RuleHostState
		}{
			{"A's rule on A's host", domain.RuleHostState{Rule: rule, Host: a.host, State: domain.StateOK, Since: T0}},
			{"A's rule on B's host", domain.RuleHostState{Rule: rule, Host: b.host, State: domain.StateOK, Since: T0}},
			{"B's rule on A's host", domain.RuleHostState{Rule: mine, Host: a.host, State: domain.StateOK, Since: T0}},
		} {
			if p := notFound("RecordTransition(B, "+c.name+")", second(st.RecordTransition(ctx, b.scope, c.st, ev))); p != "" {
				return p
			}
		}
		states := must(st.AlertStates(ctx, a.scope))
		for _, s := range states {
			if s.Rule == rule && s.State != domain.StateFiring {
				return "tenant B resolved tenant A's alert"
			}
		}
		if n := len(must(st.ListEvents(ctx, b.scope, 10))); n > 0 {
			return fmt.Sprintf("refused transitions left %d events under B", n)
		}
		return ""
	},
	"StateRepo.SetDelivery": func(ctx context.Context, st Stores, a, b fixture) string {
		_, _, ev, err := seedAlerting(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		if p := notFound("SetDelivery(B, A's event)", st.SetDelivery(ctx, b.scope, ev, "delivered", "")); p != "" {
			return p
		}
		for _, e := range must(st.ListEvents(ctx, a.scope, 100)) {
			if e.ID == ev && e.Outcome != "" {
				return "tenant B set tenant A's delivery outcome"
			}
		}
		return ""
	},
	"StateRepo.ListEvents": func(ctx context.Context, st Stores, a, b fixture) string {
		if _, _, _, err := seedAlerting(ctx, st, a); err != nil {
			return err.Error()
		}
		got, err := st.ListEvents(ctx, b.scope, 100)
		for _, e := range got {
			if e.Host == a.host {
				return "ListEvents(B) returned A's event"
			}
		}
		if err != nil {
			return err.Error()
		}
		return ""
	},
	"BucketReader.TenantBuckets": func(ctx context.Context, st Stores, a, b fixture) string {
		got, err := st.TenantBuckets(ctx, b.scope, T0) // both tenants were seeded at T0
		if err != nil {
			return err.Error()
		}
		if _, leaked := got[a.host]; leaked || len(got) != 1 {
			return fmt.Sprintf("TenantBuckets(B) = %d hosts, A's included: %v", len(got), leaked)
		}
		return ""
	},
	"HostRepo.SetHostDisabled": func(ctx context.Context, st Stores, a, b fixture) string {
		at := T0
		if p := notFound("SetHostDisabled(B, A's host)", st.SetHostDisabled(ctx, b.scope, a.host, &at)); p != "" {
			return p
		}
		if h, _ := st.GetHost(ctx, a.scope, a.host); h.DisabledAt != nil {
			return "tenant B disabled tenant A's host"
		}
		return ""
	},
}
