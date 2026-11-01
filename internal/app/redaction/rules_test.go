package redaction

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

type fakeRepo struct {
	rules     []domain.RedactionRule
	next      int64
	deleteErr error
}

func (f *fakeRepo) RedactionRules(context.Context, domain.Scope) ([]domain.RedactionRule, error) {
	return f.rules, nil
}
func (f *fakeRepo) CreateRedactionRule(_ context.Context, _ domain.Scope, r domain.RedactionRule) (int64, error) {
	f.next++
	r.ID = f.next
	f.rules = append(f.rules, r)
	return r.ID, nil
}
func (f *fakeRepo) DeleteRedactionRule(context.Context, domain.Scope, int64) error { return f.deleteErr }

type fakeInvalidator struct{ tenants []domain.TenantID }

func (f *fakeInvalidator) Invalidate(t domain.TenantID) { f.tenants = append(f.tenants, t) }

func scope(t *testing.T) (domain.Scope, domain.TenantID) {
	t.Helper()
	tid := domain.NewTenantID()
	s, err := domain.NewScope(tid)
	if err != nil {
		t.Fatal(err)
	}
	return s, tid
}

func TestCreateStoresTheRuleAndInvalidatesTheTenant(t *testing.T) {
	repo, inv := &fakeRepo{}, &fakeInvalidator{}
	s, tid := scope(t)
	got, err := NewRules(repo, inv).Create(context.Background(), s, domain.RedactionRule{Pattern: `order-\d+`, Action: domain.RedactMask})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got.ID != 1 || len(repo.rules) != 1 {
		t.Fatalf("rule %+v, stored %d, want id 1 stored once", got, len(repo.rules))
	}
	if len(inv.tenants) != 1 || inv.tenants[0] != tid {
		t.Fatalf("invalidated %v, want exactly the rule's tenant", inv.tenants)
	}
}

func TestCreateRefusesBadRulesWithoutStoringOrInvalidating(t *testing.T) {
	full := make([]domain.RedactionRule, domain.MaxRedactionRules)
	cases := []struct {
		name     string
		existing []domain.RedactionRule
		rule     domain.RedactionRule
		want     error
	}{
		{"invalid regex", nil, domain.RedactionRule{Pattern: `(unclosed`, Action: domain.RedactMask}, domain.ErrInvalid},
		{"unknown action", nil, domain.RedactionRule{Pattern: `x`, Action: "hide"}, domain.ErrInvalid},
		{"empty pattern", nil, domain.RedactionRule{Pattern: ``, Action: domain.RedactMask}, domain.ErrInvalid},
		{"at the rule limit", full, domain.RedactionRule{Pattern: `x`, Action: domain.RedactDrop}, domain.ErrQuotaExceeded},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo, inv := &fakeRepo{rules: c.existing}, &fakeInvalidator{}
			s, _ := scope(t)
			_, err := NewRules(repo, inv).Create(context.Background(), s, c.rule)
			if !errors.Is(err, c.want) {
				t.Fatalf("err %v, want %v", err, c.want)
			}
			if len(repo.rules) != len(c.existing) || len(inv.tenants) != 0 {
				t.Fatalf("refused rule stored (%d) or cache invalidated (%d)", len(repo.rules)-len(c.existing), len(inv.tenants))
			}
		})
	}
}

func TestDeleteInvalidatesOnlyWhenSomethingWasDeleted(t *testing.T) {
	cases := []struct {
		name      string
		deleteErr error
		wantInv   int
	}{
		{"deleted", nil, 1},
		{"another tenant's id", fmt.Errorf("rule: %w", domain.ErrNotFound), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo, inv := &fakeRepo{deleteErr: c.deleteErr}, &fakeInvalidator{}
			s, _ := scope(t)
			err := NewRules(repo, inv).Delete(context.Background(), s, 7)
			if !errors.Is(err, c.deleteErr) {
				t.Fatalf("err %v, want %v", err, c.deleteErr)
			}
			if len(inv.tenants) != c.wantInv {
				t.Fatalf("%d invalidations, want %d", len(inv.tenants), c.wantInv)
			}
		})
	}
}

func TestRulesWorkWithoutAnInvalidator(t *testing.T) {
	s, _ := scope(t)
	r := NewRules(&fakeRepo{}, nil)
	if _, err := r.Create(context.Background(), s, domain.RedactionRule{Pattern: `x`, Action: domain.RedactMask}); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(context.Background(), s, 1); err != nil {
		t.Fatal(err)
	}
}
