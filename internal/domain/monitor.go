package domain

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MonitorKind is how a monitor checks its target.
type MonitorKind string

const (
	MonitorHTTP MonitorKind = "http" // GET or HEAD of a URL; https adds certificate checks
	MonitorTCP  MonitorKind = "tcp"  // a connection to host:port
	MonitorPing MonitorKind = "ping" // ICMP echo to a host
)

// KeywordMode is what an http monitor requires of the response body.
type KeywordMode string

const (
	KeywordNone    KeywordMode = ""
	KeywordPresent KeywordMode = "present" // the body must contain the keyword
	KeywordAbsent  KeywordMode = "absent"  // the body must not contain it
)

// StatusRange is an inclusive range of accepted HTTP status codes.
type StatusRange struct{ From, To int }

// Monitor is one external check: a target, how to check it, how often.
type Monitor struct {
	ID       int64
	Name     string
	Kind     MonitorKind
	Target   string // http: URL; tcp: host:port; ping: host
	Interval time.Duration
	Timeout  time.Duration
	// ConfirmRetries is how many quick re-checks must also fail before the
	// monitor is down: a single failed check never opens an incident.
	ConfirmRetries int
	// http only.
	Method       string // GET or HEAD
	AcceptStatus []StatusRange
	Keyword      string
	KeywordMode  KeywordMode
	KeywordCase  bool // case-sensitive match
	CertWarnDays int  // https: warn this many days before expiry; 0 = off
	Channels     []int64
	Paused       bool
	CreatedAt    time.Time
}

// Monitor limits.
const (
	MinMonitorInterval    = 30 * time.Second
	MaxMonitorInterval    = 24 * time.Hour
	MinMonitorTimeout     = time.Second
	MaxMonitorTimeout     = 30 * time.Second
	MaxConfirmRetries     = 3
	MaxCertWarnDays       = 365
	MaxKeywordLen         = 200
	MaxMonitorTargetLen   = 2048
	MaxMonitorNameLen     = 100
	MaxMonitorChannels    = 10
	DefaultCertWarnDays   = 14
	DefaultConfirmRetries = 1
)

// DefaultAcceptStatus is what an http monitor accepts unless told otherwise:
// success and redirects (the final status, after following redirects).
var DefaultAcceptStatus = []StatusRange{{200, 399}}

// ValidateMonitor checks a monitor's shape. minInterval is the tenant's
// plan floor. Whether the target's address is allowed is decided when it is
// dialled, not here: a name can resolve differently later.
func ValidateMonitor(m Monitor, minInterval time.Duration) error {
	bad := func(f string, a ...any) error { return fmt.Errorf("%w: "+f, append([]any{ErrInvalid}, a...)...) }
	if n := strings.TrimSpace(m.Name); n == "" || len(n) > MaxMonitorNameLen {
		return bad("name must be 1-%d characters", MaxMonitorNameLen)
	}
	floor := max(minInterval, MinMonitorInterval)
	if m.Interval < floor || m.Interval > MaxMonitorInterval || m.Interval%time.Second != 0 {
		return bad("interval must be whole seconds between %s and 24h", floor)
	}
	if m.Timeout < MinMonitorTimeout || m.Timeout > MaxMonitorTimeout || m.Timeout%time.Second != 0 || m.Timeout >= m.Interval {
		return bad("timeout must be whole seconds between 1s and 30s, below the interval")
	}
	if m.ConfirmRetries < 0 || m.ConfirmRetries > MaxConfirmRetries {
		return bad("confirm_retries must be 0-%d", MaxConfirmRetries)
	}
	if len(m.Channels) > MaxMonitorChannels {
		return bad("at most %d channels", MaxMonitorChannels)
	}
	if len(m.Target) > MaxMonitorTargetLen {
		return bad("target too long")
	}
	switch m.Kind {
	case MonitorHTTP:
		return validateHTTPMonitor(m)
	case MonitorTCP, MonitorPing:
		if m.Method != "" || m.Keyword != "" || m.KeywordMode != KeywordNone || len(m.AcceptStatus) > 0 || m.CertWarnDays != 0 {
			return bad("method, status, keyword and certificate options apply to http monitors only")
		}
		if m.Kind == MonitorTCP {
			return validateHostPort(m.Target)
		}
		if !validHost(m.Target) {
			return bad("ping target must be a host name or IP address")
		}
		return nil
	default:
		return bad("kind must be http, tcp or ping")
	}
}

func validateHTTPMonitor(m Monitor) error {
	bad := func(f string, a ...any) error { return fmt.Errorf("%w: "+f, append([]any{ErrInvalid}, a...)...) }
	u, err := url.Parse(m.Target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || !validHost(u.Hostname()) {
		return bad("target must be an http or https URL")
	}
	if u.User != nil {
		return bad("target must not carry credentials")
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return bad("target port must be 1-65535")
		}
	}
	if m.Method != "GET" && m.Method != "HEAD" {
		return bad("method must be GET or HEAD")
	}
	for _, r := range m.AcceptStatus {
		if r.From < 100 || r.To > 599 || r.From > r.To {
			return bad("accepted status ranges must lie within 100-599")
		}
	}
	switch m.KeywordMode {
	case KeywordNone:
		if m.Keyword != "" {
			return bad("a keyword needs keyword_mode present or absent")
		}
	case KeywordPresent, KeywordAbsent:
		if m.Keyword == "" || len(m.Keyword) > MaxKeywordLen {
			return bad("keyword must be 1-%d characters", MaxKeywordLen)
		}
		if m.Method == "HEAD" {
			return bad("a keyword check needs GET: HEAD has no body")
		}
	default:
		return bad("keyword_mode must be present or absent")
	}
	if m.CertWarnDays < 0 || m.CertWarnDays > MaxCertWarnDays {
		return bad("cert_warn_days must be 0-%d", MaxCertWarnDays)
	}
	if m.CertWarnDays > 0 && u.Scheme != "https" {
		return bad("certificate warnings need an https target")
	}
	return nil
}

func validateHostPort(target string) error {
	host, port, err := net.SplitHostPort(target)
	if err != nil || !validHost(host) {
		return fmt.Errorf("%w: tcp target must be host:port", ErrInvalid)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("%w: tcp port must be 1-65535", ErrInvalid)
	}
	return nil
}

// hostName is a DNS name: labels of letters, digits and hyphens.
var hostName = regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*\.?$`)

// validHost accepts a DNS name or an IP literal (IPv6 without brackets).
func validHost(h string) bool {
	if a, err := netip.ParseAddr(strings.Trim(h, "[]")); err == nil {
		return a.Zone() == ""
	}
	return len(h) <= 253 && hostName.MatchString(h)
}

// Accepts reports whether status is accepted (the defaults when unset).
func (m Monitor) Accepts(status int) bool {
	ranges := m.AcceptStatus
	if len(ranges) == 0 {
		ranges = DefaultAcceptStatus
	}
	for _, r := range ranges {
		if status >= r.From && status <= r.To {
			return true
		}
	}
	return false
}

// RetryDelay is how soon a failed check is re-checked for confirmation.
func (m Monitor) RetryDelay() time.Duration { return min(10*time.Second, m.Interval/3) }

// ParseStatusRanges parses "200-299,301" into ranges.
func ParseStatusRanges(s string) ([]StatusRange, error) {
	var out []StatusRange
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		from, to, isRange := strings.Cut(part, "-")
		a, err1 := strconv.Atoi(strings.TrimSpace(from))
		b := a
		var err2 error
		if isRange {
			b, err2 = strconv.Atoi(strings.TrimSpace(to))
		}
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("%w: status ranges look like 200-299,301", ErrInvalid)
		}
		out = append(out, StatusRange{a, b})
	}
	return out, nil
}

// FormatStatusRanges is the inverse of ParseStatusRanges.
func FormatStatusRanges(rs []StatusRange) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = strconv.Itoa(r.From)
		if r.To != r.From {
			parts[i] += "-" + strconv.Itoa(r.To)
		}
	}
	return strings.Join(parts, ",")
}
