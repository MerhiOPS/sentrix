package httpapi

import (
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

type alertRuleDTO struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Metric      string   `json:"metric"`
	Aggregation string   `json:"aggregation"`
	Comparator  string   `json:"comparator"`
	Threshold   float64  `json:"threshold"`
	DurationS   int64    `json:"duration_s"`
	Hosts       []string `json:"hosts"`
	OnNoData    string   `json:"on_no_data"`
	CooldownS   int64    `json:"cooldown_s"`
	Channels    []int64  `json:"channels"`
	Enabled     bool     `json:"enabled"`
	// Inert is true when the rule matches no enabled host: it can never
	// fire, and the UI says so (OLD C-M12).
	Inert bool `json:"inert"`
}

type alertRuleListDTO struct {
	Rules []alertRuleDTO `json:"rules"`
}

type channelDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

type channelListDTO struct {
	Channels []channelDTO `json:"channels"`
}

type silenceDTO struct {
	ID     int64     `json:"id"`
	RuleID *int64    `json:"rule_id"`
	HostID *string   `json:"host_id"`
	Starts time.Time `json:"starts"`
	Ends   time.Time `json:"ends"`
	Reason string    `json:"reason"`
}

type silenceListDTO struct {
	Silences []silenceDTO `json:"silences"`
}

type alertStateDTO struct {
	RuleID int64     `json:"rule_id"`
	HostID string    `json:"host_id"`
	State  string    `json:"state"`
	Since  time.Time `json:"since"`
}

type alertEventDTO struct {
	ID       int64     `json:"id"`
	RuleID   int64     `json:"rule_id"`
	HostID   string    `json:"host_id"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	At       time.Time `json:"at"`
	Value    *float64  `json:"value"`
	Notified bool      `json:"notified"`
	Outcome  string    `json:"outcome"`
	Delivery string    `json:"delivery"`
}

type alertsDTO struct {
	States []alertStateDTO `json:"states"`
	Events []alertEventDTO `json:"events"`
}

func toAlertRule(r domain.AlertRule, inert bool) alertRuleDTO {
	hosts := make([]string, 0, len(r.Hosts))
	for _, h := range r.Hosts {
		hosts = append(hosts, h.String())
	}
	chans := append(make([]int64, 0, len(r.Channels)), r.Channels...)
	return alertRuleDTO{ID: r.ID, Name: r.Name, Kind: string(r.Kind), Metric: r.Metric, Aggregation: string(r.Aggregation),
		Comparator: string(r.Comparator), Threshold: r.Threshold, DurationS: int64(r.Duration / time.Second), Hosts: hosts,
		OnNoData: string(r.OnNoData), CooldownS: int64(r.Cooldown / time.Second), Channels: chans, Enabled: r.Enabled, Inert: inert}
}

func (d alertRuleDTO) toDomain() (domain.AlertRule, error) {
	r := domain.AlertRule{ID: d.ID, Name: d.Name, Kind: domain.RuleKind(d.Kind), Metric: d.Metric,
		Aggregation: domain.Aggregation(d.Aggregation), Comparator: domain.Comparator(d.Comparator), Threshold: d.Threshold,
		Duration: time.Duration(d.DurationS) * time.Second, OnNoData: domain.NoDataPolicy(d.OnNoData),
		Cooldown: time.Duration(d.CooldownS) * time.Second, Channels: d.Channels, Enabled: d.Enabled}
	if d.DurationS > int64(domain.MaxRuleDuration/time.Second) || d.CooldownS > int64(domain.MaxRuleDuration/time.Second) {
		return r, fmt.Errorf("%w: duration out of range", domain.ErrInvalid) // before it can overflow
	}
	for _, s := range d.Hosts {
		id, err := domain.ParseHostID(s)
		if err != nil {
			return r, fmt.Errorf("%w: host id %q", domain.ErrInvalid, s)
		}
		r.Hosts = append(r.Hosts, id)
	}
	return r, nil
}

func toChannel(c domain.Channel) channelDTO {
	return channelDTO{ID: c.ID, Name: c.Name, Kind: string(c.Kind), URL: c.URL}
}

func toSilence(s domain.Silence) silenceDTO {
	out := silenceDTO{ID: s.ID, RuleID: s.Rule, Starts: s.Starts, Ends: s.Ends, Reason: s.Reason}
	if s.Host != nil {
		h := s.Host.String()
		out.HostID = &h
	}
	return out
}

func (d silenceDTO) toDomain() (domain.Silence, error) {
	s := domain.Silence{Rule: d.RuleID, Starts: d.Starts.UTC(), Ends: d.Ends.UTC(), Reason: d.Reason}
	if d.HostID != nil {
		id, err := domain.ParseHostID(*d.HostID)
		if err != nil {
			return s, fmt.Errorf("%w: host id", domain.ErrInvalid)
		}
		s.Host = &id
	}
	return s, nil
}

func toAlerts(states []domain.RuleHostState, events []domain.AlertEvent) alertsDTO {
	out := alertsDTO{States: []alertStateDTO{}, Events: make([]alertEventDTO, 0, len(events))}
	for _, s := range states {
		if s.State != domain.StateOK {
			out.States = append(out.States, alertStateDTO{RuleID: s.Rule, HostID: s.Host.String(), State: string(s.State), Since: s.Since})
		}
	}
	for _, e := range events {
		out.Events = append(out.Events, alertEventDTO{ID: e.ID, RuleID: e.Rule, HostID: e.Host.String(), From: string(e.From),
			To: string(e.To), At: e.At, Value: e.Value, Notified: e.Notify, Outcome: e.Outcome, Delivery: e.Delivery})
	}
	return out
}

// inert reports whether a rule matches no enabled host.
func inert(r domain.AlertRule, hosts []domain.Host) bool {
	want := map[domain.HostID]bool{}
	for _, id := range r.Hosts {
		want[id] = true
	}
	for _, h := range hosts {
		if h.Enabled() && (len(want) == 0 || want[h.ID]) {
			return false
		}
	}
	return true
}
