package storetest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// seedAlerting gives f a channel, a rule on its host, a state and an event.
func seedAlerting(ctx context.Context, st Stores, f fixture) (rule, channel, event int64, err error) {
	if channel, err = st.CreateChannel(ctx, f.scope, domain.Channel{Name: "secret", Kind: domain.ChannelSlack,
		URL: "https://hooks.example/tenant-a-secret"}); err != nil {
		return
	}
	if rule, err = st.CreateRule(ctx, f.scope, alertRule([]domain.HostID{f.host}, []int64{channel})); err != nil {
		return
	}
	event, err = st.RecordTransition(ctx, f.scope, domain.RuleHostState{Rule: rule, Host: f.host, State: domain.StateFiring, Since: T0},
		&domain.AlertEvent{From: domain.StateOK, To: domain.StateFiring, At: T0})
	return
}

func notFound(call string, err error) string {
	if !errors.Is(err, domain.ErrNotFound) {
		return fmt.Sprintf("%s err = %v, want ErrNotFound", call, err)
	}
	return ""
}

// alertCrossCases extend crossCases for the Phase 5 ports. B acting on A's
// ids must find nothing and change nothing.
var alertCrossCases = map[string]crossCase{
	"RuleRepo.ListRules": func(ctx context.Context, st Stores, a, b fixture) string {
		rule, _, _, err := seedAlerting(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		got, err := st.ListRules(ctx, b.scope)
		if err != nil {
			return err.Error()
		}
		if keptRule(got, rule) == "" {
			return "ListRules(B) returned A's rule"
		}
		return ""
	},
	"RuleRepo.CreateRule": func(ctx context.Context, st Stores, a, b fixture) string {
		_, ch, _, err := seedAlerting(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		if p := notFound("CreateRule(B, A's host)", second(st.CreateRule(ctx, b.scope, alertRule([]domain.HostID{a.host}, nil)))); p != "" {
			return p
		}
		if p := notFound("CreateRule(B, A's channel)", second(st.CreateRule(ctx, b.scope, alertRule(nil, []int64{ch})))); p != "" {
			return p
		}
		for _, r := range must(st.ListRules(ctx, b.scope)) {
			if len(r.Hosts) > 0 || len(r.Channels) > 0 {
				return "a refused cross-tenant rule was stored"
			}
		}
		return ""
	},
	"RuleRepo.UpdateRule": func(ctx context.Context, st Stores, a, b fixture) string {
		rule, _, _, err := seedAlerting(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		r := alertRule(nil, nil)
		r.ID, r.Name = rule, "hijacked"
		if p := notFound("UpdateRule(B, A's rule)", st.UpdateRule(ctx, b.scope, r)); p != "" {
			return p
		}
		mine, _ := st.CreateRule(ctx, b.scope, alertRule(nil, nil))
		r.ID, r.Hosts = mine, []domain.HostID{a.host}
		if p := notFound("UpdateRule(B's rule onto A's host)", st.UpdateRule(ctx, b.scope, r)); p != "" {
			return p
		}
		for _, got := range must(st.ListRules(ctx, a.scope)) {
			if got.Name == "hijacked" {
				return "tenant B renamed tenant A's rule"
			}
		}
		return ""
	},
	"RuleRepo.DeleteRule": func(ctx context.Context, st Stores, a, b fixture) string {
		rule, _, _, err := seedAlerting(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		if p := notFound("DeleteRule(B, A's rule)", st.DeleteRule(ctx, b.scope, rule)); p != "" {
			return p
		}
		return keptRule(must(st.ListRules(ctx, a.scope)), rule)
	},
	"ChannelRepo.ListChannels": func(ctx context.Context, st Stores, a, b fixture) string {
		if _, _, _, err := seedAlerting(ctx, st, a); err != nil {
			return err.Error()
		}
		got, err := st.ListChannels(ctx, b.scope)
		if err != nil {
			return err.Error()
		}
		for _, c := range got {
			if strings.Contains(c.URL, "tenant-a-secret") {
				return "ListChannels(B) exposed A's secret URL"
			}
		}
		return ""
	},
	"ChannelRepo.CreateChannel": func(ctx context.Context, st Stores, a, b fixture) string {
		if _, err := st.CreateChannel(ctx, b.scope, domain.Channel{Name: "b-made", Kind: domain.ChannelWebhook, URL: "https://b.example/x"}); err != nil {
			return err.Error()
		}
		for _, c := range must(st.ListChannels(ctx, a.scope)) {
			if c.Name == "b-made" {
				return "a channel created under B is visible to A"
			}
		}
		return ""
	},
	"ChannelRepo.UpdateChannel": func(ctx context.Context, st Stores, a, b fixture) string {
		_, ch, _, err := seedAlerting(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		evil := domain.Channel{ID: ch, Name: "x", Kind: domain.ChannelWebhook, URL: "https://attacker.example/"}
		if p := notFound("UpdateChannel(B, A's channel)", st.UpdateChannel(ctx, b.scope, evil)); p != "" {
			return p
		}
		for _, c := range must(st.ListChannels(ctx, a.scope)) {
			if c.URL == evil.URL {
				return "tenant B redirected tenant A's channel"
			}
		}
		return ""
	},
	"ChannelRepo.DeleteChannel": func(ctx context.Context, st Stores, a, b fixture) string {
		_, ch, _, err := seedAlerting(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		return notFound("DeleteChannel(B, A's channel)", st.DeleteChannel(ctx, b.scope, ch))
	},
	"SilenceRepo.ListSilences": func(ctx context.Context, st Stores, a, b fixture) string {
		if _, err := st.CreateSilence(ctx, a.scope, domain.Silence{Starts: T0, Ends: T0.Add(time.Hour), Reason: "a-only"}); err != nil {
			return err.Error()
		}
		got, err := st.ListSilences(ctx, b.scope)
		for _, s := range got {
			if s.Reason == "a-only" {
				return "ListSilences(B) returned A's silence"
			}
		}
		if err != nil {
			return err.Error()
		}
		return ""
	},
	"SilenceRepo.CreateSilence": func(ctx context.Context, st Stores, a, b fixture) string {
		rule, _, _, err := seedAlerting(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		window := domain.Silence{Starts: T0, Ends: T0.Add(time.Hour)}
		onHost, onRule := window, window
		onHost.Host, onRule.Rule = &a.host, &rule
		if p := notFound("CreateSilence(B, A's host)", second(st.CreateSilence(ctx, b.scope, onHost))); p != "" {
			return p
		}
		return notFound("CreateSilence(B, A's rule)", second(st.CreateSilence(ctx, b.scope, onRule)))
	},
	"SilenceRepo.DeleteSilence": func(ctx context.Context, st Stores, a, b fixture) string {
		id, err := st.CreateSilence(ctx, a.scope, domain.Silence{Starts: T0, Ends: T0.Add(time.Hour)})
		if err != nil {
			return err.Error()
		}
		return notFound("DeleteSilence(B, A's silence)", st.DeleteSilence(ctx, b.scope, id))
	},
}

func second[T any](_ T, err error) error { return err }

func must[T any](v T, _ error) T { return v }

func keptRule(rules []domain.AlertRule, id int64) string {
	for _, r := range rules {
		if r.ID == id {
			return ""
		}
	}
	return "tenant B deleted tenant A's rule"
}
