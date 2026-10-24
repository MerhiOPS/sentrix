package redact

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// RuleSource loads a tenant's custom rules.
type RuleSource interface {
	RedactionRules(ctx context.Context, s domain.Scope) ([]domain.RedactionRule, error)
}

type compiled struct {
	re     *regexp.Regexp
	repl   string
	action domain.RedactionAction
}

type cacheEntry struct {
	rules   []compiled
	expires time.Time
}

// Redactor applies the built-in rules, then the tenant's own, to every field
// of a line: message, source and every label value. OLD's redaction test
// checked the message only, and labels leaked past it green (A-C2).
type Redactor struct {
	src   RuleSource
	ttl   time.Duration
	now   func() time.Time
	mu    sync.Mutex
	cache map[domain.TenantID]cacheEntry
}

// New builds a redactor caching each tenant's compiled rules for ttl.
func New(src RuleSource, ttl time.Duration, now func() time.Time) *Redactor {
	return &Redactor{src: src, ttl: ttl, now: now, cache: map[domain.TenantID]cacheEntry{}}
}

// Invalidate drops a tenant's cached rules (call after a rule change).
func (r *Redactor) Invalidate(t domain.TenantID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cache, t)
}

// Redact implements ingest.Redactor. It returns the lines to keep and how
// many were changed or dropped.
func (r *Redactor) Redact(ctx context.Context, s domain.Scope, lines []domain.LogLine) ([]domain.LogLine, int, int, error) {
	rules, err := r.rulesFor(ctx, s)
	if err != nil {
		return nil, 0, 0, err
	}
	kept := make([]domain.LogLine, 0, len(lines))
	redacted, dropped := 0, 0
	for _, l := range lines {
		out, changed, drop := redactLine(l, rules)
		if drop {
			dropped++
			continue
		}
		if changed {
			redacted++
		}
		kept = append(kept, out)
	}
	return kept, redacted, dropped, nil
}

func (r *Redactor) rulesFor(ctx context.Context, s domain.Scope) ([]compiled, error) {
	t, err := s.Tenant()
	if err != nil {
		return nil, err
	}
	now := r.now()
	r.mu.Lock()
	e, ok := r.cache[t]
	r.mu.Unlock()
	if ok && now.Before(e.expires) {
		return e.rules, nil
	}
	raw, err := r.src.RedactionRules(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("load redaction rules: %w", err)
	}
	rules := make([]compiled, 0, len(raw))
	for _, rr := range raw {
		re, err := domain.CompileRule(rr)
		if err != nil {
			continue // validated on save; a rule that no longer compiles is skipped, not fatal
		}
		repl := rr.Replacement
		if repl == "" {
			repl = domain.DefaultRedactionReplace
		}
		rules = append(rules, compiled{re: re, repl: repl, action: rr.Action})
	}
	r.mu.Lock()
	r.cache[t] = cacheEntry{rules: rules, expires: now.Add(r.ttl)}
	r.mu.Unlock()
	return rules, nil
}

// redactLine applies built-ins then custom rules to every field. A drop
// rule matching any field drops the whole line.
func redactLine(l domain.LogLine, rules []compiled) (domain.LogLine, bool, bool) {
	fields := []*string{&l.Message, &l.Source}
	labels := make(domain.Labels, len(l.Labels))
	for k, v := range l.Labels {
		labels[k] = v
	}
	l.Labels = labels
	changed := false
	for _, f := range fields {
		if apply(f, rules, &changed) {
			return l, changed, true
		}
	}
	for k, v := range labels {
		if apply(&v, rules, &changed) {
			return l, changed, true
		}
		labels[k] = v
	}
	return l, changed, false
}

// apply redacts one field in place; it reports a drop.
func apply(f *string, rules []compiled, changed *bool) bool {
	orig := *f
	s := applyBuiltins(orig)
	for _, c := range rules {
		if !c.re.MatchString(s) {
			continue
		}
		if c.action == domain.RedactDrop {
			return true
		}
		s = c.re.ReplaceAllString(s, c.repl)
	}
	if s != orig {
		*f, *changed = s, true
	}
	return false
}
