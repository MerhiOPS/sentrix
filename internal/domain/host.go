package domain

import "time"

// Host is a machine running the agent, owned by exactly one tenant.
type Host struct {
	ID           HostID
	Tenant       TenantID
	Name         string
	OS           string // "linux" | "windows"
	Platform     string // distribution / edition, informational
	AgentVersion string
	Interval     time.Duration // sampling interval the agent reports
	CreatedAt    time.Time
	LastSeen     time.Time  // zero until the first batch is stored
	DisabledAt   *time.Time // nil while enabled
	Health       HostHealth
}

// HostHealth is the agent's self-report plus what the server refused, so an
// operator can tell "quiet host" from "host whose data is being dropped".
type HostHealth struct {
	BufferDepth   int           // batches waiting on the agent, last report
	DroppedTotal  uint64        // batches the agent dropped since it started
	ClockSkew     time.Duration // agent clock minus server clock
	RejectedTotal uint64        // samples the server refused (clock, validity)
	ConflictTotal uint64        // samples whose timestamp was already stored
}

// Enabled reports whether the host may ingest and be evaluated.
func (h Host) Enabled() bool { return h.DisabledAt == nil }

// HostStatus is what the dashboard can honestly say about a host. With push
// collection "agent down" and "network down" look identical, so there is no
// "down" state — only how long it has been since data arrived.
type HostStatus string

const (
	StatusOK       HostStatus = "ok"
	StatusStale    HostStatus = "stale"   // > 3 intervals without data
	StatusNoData   HostStatus = "no_data" // > 10 intervals, or never seen
	StatusDisabled HostStatus = "disabled"
)

// Liveness thresholds, in multiples of the host's sampling interval.
const (
	staleAfterIntervals  = 3
	noDataAfterIntervals = 10
)

// DefaultInterval is assumed when a host has not reported its interval.
const DefaultInterval = 5 * time.Second

// Status derives the host's liveness at now.
func (h Host) Status(now time.Time) HostStatus {
	if !h.Enabled() {
		return StatusDisabled
	}
	if h.LastSeen.IsZero() {
		return StatusNoData
	}
	interval := h.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	age := now.Sub(h.LastSeen)
	switch {
	case age > noDataAfterIntervals*interval:
		return StatusNoData
	case age > staleAfterIntervals*interval:
		return StatusStale
	default:
		return StatusOK
	}
}
