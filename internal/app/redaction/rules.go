// Package redaction manages a tenant's own redaction rules (the built-in
// rules are always on and live in infra/redact).
package redaction

import (
	"context"
	"fmt"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// RuleRepo stores rules.
type RuleRepo interface {
	RedactionRules(ctx context.Context, s domain.Scope) ([]domain.RedactionRule, error)
	CreateRedactionRule(ctx context.Context, s domain.Scope, r domain.RedactionRule) (int64, error)
	DeleteRedactionRule(ctx context.Context, s domain.Scope, id int64) error
}

// Invalidator is told when a tenant's rules change, so ingest stops using a
// cached copy immediately rather than after its TTL.
type Invalidator interface {
	Invalidate(t domain.TenantID)
}

// Rules is the rule-management use case.
type Rules struct {
	repo RuleRepo
	inv  Invalidator
}

// NewRules builds the use case.
func NewRules(repo RuleRepo, inv Invalidator) *Rules { return &Rules{repo: repo, inv: inv} }

// List returns the tenant's rules.
func (r *Rules) List(ctx context.Context, s domain.Scope) ([]domain.RedactionRule, error) {
	return r.repo.RedactionRules(ctx, s)
}

// Create validates (RE2, bounded) and stores a rule.
func (r *Rules) Create(ctx context.Context, s domain.Scope, rule domain.RedactionRule) (domain.RedactionRule, error) {
	if _, err := domain.CompileRule(rule); err != nil {
		return domain.RedactionRule{}, err
	}
	existing, err := r.repo.RedactionRules(ctx, s)
	if err != nil {
		return domain.RedactionRule{}, err
	}
	if len(existing) >= domain.MaxRedactionRules {
		return domain.RedactionRule{}, fmt.Errorf("%w: at most %d rules", domain.ErrQuotaExceeded, domain.MaxRedactionRules)
	}
	if rule.ID, err = r.repo.CreateRedactionRule(ctx, s, rule); err != nil {
		return domain.RedactionRule{}, err
	}
	r.invalidate(s)
	return rule, nil
}

// Delete removes a rule; another tenant's rule id is not found.
func (r *Rules) Delete(ctx context.Context, s domain.Scope, id int64) error {
	if err := r.repo.DeleteRedactionRule(ctx, s, id); err != nil {
		return err
	}
	r.invalidate(s)
	return nil
}

func (r *Rules) invalidate(s domain.Scope) {
	if t, err := s.Tenant(); err == nil && r.inv != nil {
		r.inv.Invalidate(t)
	}
}
