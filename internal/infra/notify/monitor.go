package notify

import (
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/alerting"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// monitorPayload is the documented uptime webhook body (docs/openapi.yaml,
// MonitorWebhook).
type monitorPayload struct {
	Source        string `json:"source"`
	Monitor       string `json:"monitor"`
	Target        string `json:"target"`
	Event         string `json:"event"`
	At            string `json:"at"`
	Resolved      bool   `json:"resolved"`
	Error         string `json:"error,omitempty"`
	DownForS      *int64 `json:"down_for_s,omitempty"`
	CertExpiresAt string `json:"cert_expires_at,omitempty"`
}

func monitorBody(m alerting.MonitorMessage, at time.Time) monitorPayload {
	p := monitorPayload{Source: "monitor", Monitor: m.Name, Target: m.Target, Event: string(m.Event),
		At: at.UTC().Format(time.RFC3339), Resolved: m.Event == domain.EventUp || m.Event == domain.EventCertRenewed,
		Error: string(m.Class)}
	if m.Event == domain.EventUp {
		s := int64(m.DownFor / time.Second)
		p.DownForS = &s
	}
	if m.CertExpiry != nil {
		p.CertExpiresAt = m.CertExpiry.UTC().Format(time.RFC3339)
	}
	return p
}

// classText says what a failure class means, for people reading Slack.
var classText = map[domain.ErrorClass]string{
	domain.ErrDNS:      "DNS lookup failed",
	domain.ErrConnect:  "connection failed",
	domain.ErrTimeout:  "timed out",
	domain.ErrTLS:      "TLS or certificate error",
	domain.ErrStatus:   "unexpected HTTP status",
	domain.ErrKeyword:  "keyword check failed",
	domain.ErrBlocked:  "address not allowed by the server",
	domain.ErrPingLoss: "no ping replies",
}

func slackMonitorText(m alerting.MonitorMessage, at time.Time) string {
	name := slackEscape.Replace(m.Name) + " (" + slackEscape.Replace(m.Target) + ")"
	when := " at " + at.UTC().Format(time.RFC3339)
	switch m.Event {
	case domain.EventDown:
		reason := classText[m.Class]
		if reason == "" {
			reason = "check failed"
		}
		return ":red_circle: DOWN — " + name + ": " + reason + when
	case domain.EventUp:
		return ":large_green_circle: UP — " + name + " after " + m.DownFor.Round(time.Second).String() + " down" + when
	case domain.EventCertExpiring:
		return fmt.Sprintf(":warning: CERTIFICATE EXPIRING — %s on %s (%d days)", name, certDate(m.CertExpiry), daysLeft(m.CertExpiry, at))
	default:
		return ":white_check_mark: CERTIFICATE RENEWED — " + name + ", valid until " + certDate(m.CertExpiry)
	}
}

func certDate(t *time.Time) string {
	if t == nil {
		return "an unknown date"
	}
	return t.UTC().Format("2006-01-02")
}

func daysLeft(t *time.Time, at time.Time) int {
	if t == nil {
		return 0
	}
	return int(t.Sub(at) / (24 * time.Hour))
}
