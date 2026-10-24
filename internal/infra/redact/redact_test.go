package redact

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

type rules []domain.RedactionRule

func (r rules) RedactionRules(context.Context, domain.Scope) ([]domain.RedactionRule, error) {
	return r, nil
}

func scope(t *testing.T) domain.Scope {
	t.Helper()
	s, err := domain.NewScope(domain.NewTenantID())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBuiltinsRedactSecretsAndPII(t *testing.T) {
	for in, want := range map[string]string{
		"GET / Authorization: Bearer eyJhbGciOi.eyJzdWIiOi.SflKxw": "GET / Authorization: [REDACTED]",
		"token is bearer abcdefghijklmnop":                         "token is bearer [REDACTED]",
		"connect postgres://app:s3cr3t@db.internal:5432/x":         "connect postgres://app:[REDACTED]@db.internal:5432/x",
		"login password=hunter22 user=bob":                         "login password=[REDACTED] user=bob",
		`cfg api_key: "AKIA1234567890" ok`:                         `cfg api_key: [REDACTED] ok`,
		"Server=db;User Id=sa;Pwd=Pa55word;":                       "Server=db;User Id=sa;Pwd=[REDACTED];",
		"mail to jane.doe+x@example.co.uk failed":                  "mail to [EMAIL] failed",
		"peer 10.0.0.12 refused":                                   "peer [IP] refused",
		"peer ::1 and fe80::1%eth0 and 2001:db8::8a2e:370:7334":    "peer [IP] and [IP] and [IP]",
		"at 12:34:56 retry":                                        "at 12:34:56 retry",         // a time, not IPv6 (OLD C-M10)
		"nic aa:bb:cc:dd:ee:ff up":                                 "nic aa:bb:cc:dd:ee:ff up",  // a MAC, not IPv6
		"version 999.1.1.1 shipped":                                "version 999.1.1.1 shipped", // not a valid IPv4
		"plain line with nothing secret":                           "plain line with nothing secret",
	} {
		if got := applyBuiltins(in); got != want {
			t.Errorf("applyBuiltins(%q)\n got  %q\n want %q", in, got, want)
		}
	}
}

func TestEveryFieldIsRedacted(t *testing.T) {
	r := New(rules(nil), time.Minute, time.Now)
	line := domain.LogLine{
		Message: "ok", Source: "/var/log/app-password=leaky.log",
		Labels: domain.Labels{"user": "jane@example.com", "peer": "10.1.2.3", "svc": "api"},
	}
	kept, redacted, dropped, err := r.Redact(context.Background(), scope(t), []domain.LogLine{line})
	if err != nil || len(kept) != 1 || redacted != 1 || dropped != 0 {
		t.Fatalf("kept %d redacted %d dropped %d err %v", len(kept), redacted, dropped, err)
	}
	k := kept[0]
	if strings.Contains(k.Source, "leaky") || k.Labels["user"] != maskEmail || k.Labels["peer"] != maskIP || k.Labels["svc"] != "api" {
		t.Fatalf("source %q labels %v: secrets in source/labels survived (OLD A-C2)", k.Source, k.Labels)
	}
	if line.Labels["user"] != "jane@example.com" {
		t.Fatal("Redact mutated the caller's labels map")
	}
}

func TestCustomRulesMaskAndDropOnAnyField(t *testing.T) {
	r := New(rules{
		{ID: 1, Pattern: `acct-\d{6}`, Replacement: "acct-######", Action: domain.RedactMask},
		{ID: 2, Pattern: `^healthcheck$`, Action: domain.RedactDrop},
	}, time.Minute, time.Now)
	lines := []domain.LogLine{
		{Message: "charged acct-123456 ok"},
		{Message: "GET /health", Labels: domain.Labels{"route": "healthcheck"}}, // drop via a label
		{Message: "untouched"},
	}
	kept, redacted, dropped, err := r.Redact(context.Background(), scope(t), lines)
	if err != nil || len(kept) != 2 || redacted != 1 || dropped != 1 {
		t.Fatalf("kept %d redacted %d dropped %d err %v", len(kept), redacted, dropped, err)
	}
	if kept[0].Message != "charged acct-###### ok" {
		t.Fatalf("custom mask: %q", kept[0].Message)
	}
}

type countingSource struct {
	n int
	r rules
}

func (c *countingSource) RedactionRules(ctx context.Context, s domain.Scope) ([]domain.RedactionRule, error) {
	c.n++
	return c.r, nil
}

func TestRulesCachedPerTenantAndInvalidated(t *testing.T) {
	src := &countingSource{}
	now := time.Now()
	r := New(src, time.Minute, func() time.Time { return now })
	s := scope(t)
	for i := 0; i < 3; i++ {
		_, _, _, _ = r.Redact(context.Background(), s, []domain.LogLine{{Message: "x"}})
	}
	if src.n != 1 {
		t.Fatalf("rules loaded %d times, want 1 (cached)", src.n)
	}
	tid, _ := s.Tenant()
	r.Invalidate(tid)
	_, _, _, _ = r.Redact(context.Background(), s, []domain.LogLine{{Message: "x"}})
	if src.n != 2 {
		t.Fatal("Invalidate did not force a reload")
	}
}
