package domain

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ChannelKind is how a notification is delivered.
type ChannelKind string

const (
	ChannelWebhook ChannelKind = "webhook"
	ChannelSlack   ChannelKind = "slack"
)

// Channel is a notification destination. Its URL is a secret: a Slack
// webhook URL is a credential, and is never returned by the API.
type Channel struct {
	ID   int64
	Name string
	Kind ChannelKind
	URL  string
}

// MaskedURL is what the API shows instead of the URL.
func (c Channel) MaskedURL() string {
	u, err := url.Parse(c.URL)
	if err != nil || u.Host == "" {
		return MaskSentinel
	}
	return u.Scheme + "://" + u.Host + "/" + MaskSentinel
}

// MaskSentinel marks a secret the client did not change: an update carrying
// a masked URL keeps the stored one (OLD B2).
const MaskSentinel = "••••"

// IsMasked reports whether a URL from a client is the masked placeholder.
func IsMasked(u string) bool { return strings.Contains(u, MaskSentinel) }

// ValidateChannel checks a channel. Destinations are https only: a webhook
// carries alert content off the box. The SSRF guard checks the address at
// connection time; this only checks the shape.
func ValidateChannel(c Channel) error {
	if n := strings.TrimSpace(c.Name); n == "" || len(n) > 100 {
		return fmt.Errorf("%w: channel name must be 1-100 characters", ErrInvalid)
	}
	if c.Kind != ChannelWebhook && c.Kind != ChannelSlack {
		return fmt.Errorf("%w: channel kind must be webhook or slack", ErrInvalid)
	}
	u, err := url.Parse(c.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || len(c.URL) > 2048 || u.User != nil {
		return fmt.Errorf("%w: channel url must be an https URL without credentials", ErrInvalid)
	}
	return nil
}

// Silence suppresses notifications — never transitions — for a rule, a
// host, or both, within [Starts, Ends).
type Silence struct {
	ID     int64
	Rule   *int64  // nil: every rule
	Host   *HostID // nil: every host
	Starts time.Time
	Ends   time.Time
	Reason string
}

// Covers reports whether the silence applies to (rule, host) at t.
func (s Silence) Covers(rule int64, host HostID, t time.Time) bool {
	if t.Before(s.Starts) || !t.Before(s.Ends) {
		return false
	}
	return (s.Rule == nil || *s.Rule == rule) && (s.Host == nil || *s.Host == host)
}

// ValidateSilence checks a silence.
func ValidateSilence(s Silence) error {
	if !s.Ends.After(s.Starts) || s.Ends.Sub(s.Starts) > 30*24*time.Hour {
		return fmt.Errorf("%w: a silence must end after it starts, within 30 days", ErrInvalid)
	}
	if len(s.Reason) > 200 {
		return fmt.Errorf("%w: reason too long", ErrInvalid)
	}
	return nil
}
