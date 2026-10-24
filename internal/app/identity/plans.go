package identity

import (
	"context"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// PlanRepo stores a tenant's plan and reads its metered usage. Setting a
// plan is an operator action (OpsScope); reading is the tenant's own.
type PlanRepo interface {
	SetPlan(ctx context.Context, ops domain.OpsScope, t domain.TenantID, p domain.Plan) error
	GetPlan(ctx context.Context, s domain.Scope) (domain.Plan, error)
	// GetUsage reads usage as of now: today and the calendar month (UTC).
	GetUsage(ctx context.Context, s domain.Scope, now time.Time) (domain.Usage, error)
}

// Plans is the plan use case: the operator sets, the tenant reads.
type Plans struct {
	repo PlanRepo
	now  func() time.Time
}

// NewPlans builds the use case.
func NewPlans(repo PlanRepo, now func() time.Time) *Plans { return &Plans{repo: repo, now: now} }

// Set validates and stores a tenant's plan (`sentrix-server tenant plan`).
func (p *Plans) Set(ctx context.Context, t domain.TenantID, plan domain.Plan) error {
	if err := domain.ValidatePlan(plan); err != nil {
		return err
	}
	ops, err := domain.NewOpsScope("set tenant plan")
	if err != nil {
		return err
	}
	return p.repo.SetPlan(ctx, ops, t, plan)
}

// Get returns the tenant's plan and usage, for Settings: limits are shown
// before they are hit.
func (p *Plans) Get(ctx context.Context, s domain.Scope) (domain.Plan, domain.Usage, error) {
	plan, err := p.repo.GetPlan(ctx, s)
	if err != nil {
		return domain.Plan{}, domain.Usage{}, err
	}
	u, err := p.repo.GetUsage(ctx, s, p.now())
	return plan, u, err
}
