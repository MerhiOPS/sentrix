package alerting

import (
	"context"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// RuleRepo stores alert rules. A rule naming a host or channel the tenant
// does not own is domain.ErrNotFound: tenant A cannot point a rule at
// tenant B's host.
type RuleRepo interface {
	ListRules(ctx context.Context, s domain.Scope) ([]domain.AlertRule, error)
	CreateRule(ctx context.Context, s domain.Scope, r domain.AlertRule) (int64, error)
	UpdateRule(ctx context.Context, s domain.Scope, r domain.AlertRule) error
	DeleteRule(ctx context.Context, s domain.Scope, id int64) error
}

// ChannelRepo stores notification channels, URLs included: masking is the
// API's job, the evaluator needs the real URL.
type ChannelRepo interface {
	ListChannels(ctx context.Context, s domain.Scope) ([]domain.Channel, error)
	CreateChannel(ctx context.Context, s domain.Scope, c domain.Channel) (int64, error)
	UpdateChannel(ctx context.Context, s domain.Scope, c domain.Channel) error
	DeleteChannel(ctx context.Context, s domain.Scope, id int64) error
}

// SilenceRepo stores silences. A silence naming another tenant's rule or
// host is domain.ErrNotFound.
type SilenceRepo interface {
	ListSilences(ctx context.Context, s domain.Scope) ([]domain.Silence, error)
	CreateSilence(ctx context.Context, s domain.Scope, sil domain.Silence) (int64, error)
	DeleteSilence(ctx context.Context, s domain.Scope, id int64) error
}

// StateRepo persists evaluation state and events.
type StateRepo interface {
	AlertStates(ctx context.Context, s domain.Scope) ([]domain.RuleHostState, error)
	// RecordTransition writes ev (when non-nil) and then st in one
	// transaction, returning the event id (0 without an event). An event is
	// never lost to a crash between the two writes (R1).
	RecordTransition(ctx context.Context, s domain.Scope, st domain.RuleHostState, ev *domain.AlertEvent) (int64, error)
	// SetDelivery records an event's notification outcome.
	SetDelivery(ctx context.Context, s domain.Scope, event int64, outcome, delivery string) error
	// ListEvents returns the newest events first, at most limit.
	ListEvents(ctx context.Context, s domain.Scope, limit int) ([]domain.AlertEvent, error)
}

// BucketReader reads one 1-minute core rollup bucket for every host of the
// tenant at once: one query per tenant per evaluation, not one per host.
type BucketReader interface {
	TenantBuckets(ctx context.Context, s domain.Scope, start time.Time) (map[domain.HostID]query.RollupBucket, error)
}

// TenantLister enumerates tenants for the evaluation loop: the one place
// alerting crosses tenants, so it takes an OpsScope.
type TenantLister interface {
	ListTenants(ctx context.Context, ops domain.OpsScope) ([]domain.TenantID, error)
}

// Message is what a notifier sends.
type Message struct {
	Rule     string
	Host     string
	State    domain.AlertState
	Metric   string
	Value    *float64
	At       time.Time
	Resolved bool
}

// Notifier delivers a message to one channel.
type Notifier interface {
	Notify(ctx context.Context, ch domain.Channel, m Message) error
}
