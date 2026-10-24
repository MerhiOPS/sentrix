package storetest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// logCrossCases extend crossCases for the Phase 4 ports.
var logCrossCases = map[string]crossCase{
	"LogReader.SearchLogs": func(ctx context.Context, st Stores, a, b fixture) string {
		if _, err := st.CommitBatch(ctx, a.scope, a.host, 601, withLogs(nil, line(0, domain.LevelError, "tenant-a-secret-line"))); err != nil {
			return err.Error()
		}
		clauses, _ := query.ParseSearch("tenant-a-secret")
		for _, hosts := range [][]domain.HostID{nil, {a.host}} {
			got, err := st.SearchLogs(ctx, b.scope, query.LogQuery{Hosts: hosts, Clauses: clauses,
				From: T0.Add(-time.Hour), To: T0.Add(time.Hour), Limit: 10})
			if err != nil || len(got) > 0 {
				return fmt.Sprintf("SearchLogs(B, hosts %v) found %d of A's lines (%v)", hosts, len(got), err)
			}
		}
		return ""
	},
	"RuleRepo.RedactionRules": func(ctx context.Context, st Stores, a, b fixture) string {
		if _, err := st.CreateRedactionRule(ctx, a.scope, domain.RedactionRule{Pattern: "a-only", Action: domain.RedactMask}); err != nil {
			return err.Error()
		}
		got, err := st.RedactionRules(ctx, b.scope)
		for _, r := range got {
			if r.Pattern == "a-only" {
				return "RedactionRules(B) returned A's rule"
			}
		}
		if err != nil {
			return err.Error()
		}
		return ""
	},
	"RuleRepo.CreateRedactionRule": func(ctx context.Context, st Stores, a, b fixture) string {
		if _, err := st.CreateRedactionRule(ctx, b.scope, domain.RedactionRule{Pattern: "b-made", Action: domain.RedactMask}); err != nil {
			return err.Error()
		}
		got, _ := st.RedactionRules(ctx, a.scope)
		for _, r := range got {
			if r.Pattern == "b-made" {
				return "a rule created under B is visible to A"
			}
		}
		return ""
	},
	"RuleRepo.DeleteRedactionRule": func(ctx context.Context, st Stores, a, b fixture) string {
		id, err := st.CreateRedactionRule(ctx, a.scope, domain.RedactionRule{Pattern: "keep-me", Action: domain.RedactMask})
		if err != nil {
			return err.Error()
		}
		if err := st.DeleteRedactionRule(ctx, b.scope, id); !errors.Is(err, domain.ErrNotFound) {
			return fmt.Sprintf("DeleteRedactionRule(B, A's rule) err = %v, want ErrNotFound", err)
		}
		got, _ := st.RedactionRules(ctx, a.scope)
		for _, r := range got {
			if r.ID == id {
				return ""
			}
		}
		return "tenant B deleted tenant A's rule"
	},
}
