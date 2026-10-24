package alerting

import (
	"context"
	"fmt"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Per-tenant limits.
const (
	MaxRules    = 200
	MaxChannels = 50
	MaxSilences = 200
	MaxEvents   = 500 // newest events returned per request
)

// Admin is the rule, channel and silence management use case. Channel URLs
// leave it masked, for every role (OLD B2): nothing after the evaluator
// needs the real one.
type Admin struct {
	rules    RuleRepo
	channels ChannelRepo
	silences SilenceRepo
	states   StateRepo
}

// NewAdmin builds the use case.
func NewAdmin(r RuleRepo, c ChannelRepo, s SilenceRepo, st StateRepo) *Admin {
	return &Admin{rules: r, channels: c, silences: s, states: st}
}

// Rules lists the tenant's rules.
func (a *Admin) Rules(ctx context.Context, s domain.Scope) ([]domain.AlertRule, error) {
	return a.rules.ListRules(ctx, s)
}

// CreateRule validates and stores a rule.
func (a *Admin) CreateRule(ctx context.Context, s domain.Scope, r domain.AlertRule) (domain.AlertRule, error) {
	if err := domain.ValidateRule(r, query.IsKnownMetric); err != nil {
		return domain.AlertRule{}, err
	}
	existing, err := a.rules.ListRules(ctx, s)
	if err != nil {
		return domain.AlertRule{}, err
	}
	if len(existing) >= MaxRules {
		return domain.AlertRule{}, fmt.Errorf("%w: at most %d alert rules", domain.ErrQuotaExceeded, MaxRules)
	}
	if r.ID, err = a.rules.CreateRule(ctx, s, r); err != nil {
		return domain.AlertRule{}, err
	}
	return r, nil
}

// UpdateRule validates and replaces a rule.
func (a *Admin) UpdateRule(ctx context.Context, s domain.Scope, r domain.AlertRule) error {
	if err := domain.ValidateRule(r, query.IsKnownMetric); err != nil {
		return err
	}
	return a.rules.UpdateRule(ctx, s, r)
}

// DeleteRule removes a rule with its state and events.
func (a *Admin) DeleteRule(ctx context.Context, s domain.Scope, id int64) error {
	return a.rules.DeleteRule(ctx, s, id)
}

// Channels lists the tenant's channels with masked URLs.
func (a *Admin) Channels(ctx context.Context, s domain.Scope) ([]domain.Channel, error) {
	cs, err := a.channels.ListChannels(ctx, s)
	for i := range cs {
		cs[i].URL = cs[i].MaskedURL()
	}
	return cs, err
}

// CreateChannel validates and stores a channel; the result is masked.
func (a *Admin) CreateChannel(ctx context.Context, s domain.Scope, c domain.Channel) (domain.Channel, error) {
	if domain.IsMasked(c.URL) {
		return domain.Channel{}, fmt.Errorf("%w: a new channel needs its real URL", domain.ErrInvalid)
	}
	if err := domain.ValidateChannel(c); err != nil {
		return domain.Channel{}, err
	}
	existing, err := a.channels.ListChannels(ctx, s)
	if err != nil {
		return domain.Channel{}, err
	}
	if len(existing) >= MaxChannels {
		return domain.Channel{}, fmt.Errorf("%w: at most %d channels", domain.ErrQuotaExceeded, MaxChannels)
	}
	if c.ID, err = a.channels.CreateChannel(ctx, s, c); err != nil {
		return domain.Channel{}, err
	}
	c.URL = c.MaskedURL()
	return c, nil
}

// UpdateChannel replaces a channel. A URL still carrying the mask keeps the
// stored secret, so a client can rename a channel it cannot read.
func (a *Admin) UpdateChannel(ctx context.Context, s domain.Scope, c domain.Channel) error {
	if domain.IsMasked(c.URL) {
		cur, err := a.channel(ctx, s, c.ID)
		if err != nil {
			return err
		}
		c.URL = cur.URL
	}
	if err := domain.ValidateChannel(c); err != nil {
		return err
	}
	return a.channels.UpdateChannel(ctx, s, c)
}

func (a *Admin) channel(ctx context.Context, s domain.Scope, id int64) (domain.Channel, error) {
	cs, err := a.channels.ListChannels(ctx, s)
	if err != nil {
		return domain.Channel{}, err
	}
	for _, c := range cs {
		if c.ID == id {
			return c, nil
		}
	}
	return domain.Channel{}, fmt.Errorf("channel: %w", domain.ErrNotFound)
}

// DeleteChannel removes a channel; rules naming it stop sending to it.
func (a *Admin) DeleteChannel(ctx context.Context, s domain.Scope, id int64) error {
	return a.channels.DeleteChannel(ctx, s, id)
}

// Silences lists the tenant's silences.
func (a *Admin) Silences(ctx context.Context, s domain.Scope) ([]domain.Silence, error) {
	return a.silences.ListSilences(ctx, s)
}

// CreateSilence validates and stores a silence.
func (a *Admin) CreateSilence(ctx context.Context, s domain.Scope, sil domain.Silence) (domain.Silence, error) {
	if err := domain.ValidateSilence(sil); err != nil {
		return domain.Silence{}, err
	}
	existing, err := a.silences.ListSilences(ctx, s)
	if err != nil {
		return domain.Silence{}, err
	}
	if len(existing) >= MaxSilences {
		return domain.Silence{}, fmt.Errorf("%w: at most %d silences", domain.ErrQuotaExceeded, MaxSilences)
	}
	if sil.ID, err = a.silences.CreateSilence(ctx, s, sil); err != nil {
		return domain.Silence{}, err
	}
	return sil, nil
}

// DeleteSilence ends a silence by removing it.
func (a *Admin) DeleteSilence(ctx context.Context, s domain.Scope, id int64) error {
	return a.silences.DeleteSilence(ctx, s, id)
}

// States lists every (rule, host) state that has left ok at least once.
func (a *Admin) States(ctx context.Context, s domain.Scope) ([]domain.RuleHostState, error) {
	return a.states.AlertStates(ctx, s)
}

// Events lists the newest events.
func (a *Admin) Events(ctx context.Context, s domain.Scope, limit int) ([]domain.AlertEvent, error) {
	if limit <= 0 || limit > MaxEvents {
		limit = MaxEvents
	}
	return a.states.ListEvents(ctx, s, limit)
}
