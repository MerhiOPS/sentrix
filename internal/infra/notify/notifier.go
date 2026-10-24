package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/alerting"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Notifier implements alerting.Notifier for webhook and Slack channels.
type Notifier struct {
	p *poster
}

// New returns a notifier behind the SSRF guard.
func New() *Notifier { return &Notifier{p: newPoster(control, nil)} }

// Notify implements alerting.Notifier.
func (n *Notifier) Notify(ctx context.Context, ch domain.Channel, m alerting.Message) error {
	var body []byte
	var err error
	switch ch.Kind {
	case domain.ChannelSlack:
		body, err = json.Marshal(map[string]string{"text": slackText(m)})
	case domain.ChannelWebhook:
		body, err = json.Marshal(webhookBody(m))
	default:
		return fmt.Errorf("unknown channel kind %q", ch.Kind)
	}
	if err != nil {
		return err
	}
	return n.p.post(ctx, ch.URL, body)
}

// webhookPayload is the documented webhook body (docs/openapi.yaml,
// AlertWebhook).
type webhookPayload struct {
	Rule     string   `json:"rule"`
	Host     string   `json:"host"`
	State    string   `json:"state"`
	Metric   string   `json:"metric,omitempty"`
	Value    *float64 `json:"value,omitempty"`
	At       string   `json:"at"`
	Resolved bool     `json:"resolved"`
}

func webhookBody(m alerting.Message) webhookPayload {
	return webhookPayload{Rule: m.Rule, Host: m.Host, State: string(m.State), Metric: m.Metric, Value: m.Value,
		At: m.At.UTC().Format(time.RFC3339), Resolved: m.Resolved}
}

func slackText(m alerting.Message) string {
	verb := ":rotating_light: FIRING"
	if m.Resolved {
		verb = ":white_check_mark: RESOLVED"
	}
	s := fmt.Sprintf("%s — %s on %s", verb, slackEscape.Replace(m.Rule), slackEscape.Replace(m.Host))
	if m.Value != nil {
		s += " (" + m.Metric + " = " + strconv.FormatFloat(*m.Value, 'f', 2, 64) + ")"
	}
	return s + " at " + m.At.UTC().Format(time.RFC3339)
}

// slackEscape stops a rule or host name from becoming a Slack control
// sequence such as <!channel>.
var slackEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
