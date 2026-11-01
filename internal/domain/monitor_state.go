package domain

import "time"

// MonitorStatus is where a monitor is.
type MonitorStatus string

const (
	MonitorPending MonitorStatus = "pending" // no confirmed result since creation or resume
	MonitorUp      MonitorStatus = "up"
	MonitorDown    MonitorStatus = "down"
	MonitorPaused  MonitorStatus = "paused"
)

// CertStatus is an https monitor's certificate state, independent of up/down.
type CertStatus string

const (
	CertUnknown  CertStatus = ""
	CertOK       CertStatus = "ok"
	CertExpiring CertStatus = "expiring"
)

// CheckOutcome is what one check found.
type CheckOutcome string

const (
	CheckUp   CheckOutcome = "up"
	CheckDown CheckOutcome = "down"
	// CheckUnknown means the server could not tell: its own network was
	// down (the reference targets failed too). It changes no state.
	CheckUnknown CheckOutcome = "unknown"
)

// ErrorClass is the only failure detail stored or shown: never a response
// body, never a raw dialer error that could name internal addresses.
type ErrorClass string

const (
	ErrNone     ErrorClass = ""
	ErrDNS      ErrorClass = "dns"
	ErrConnect  ErrorClass = "connect"
	ErrTimeout  ErrorClass = "timeout"
	ErrTLS      ErrorClass = "tls"
	ErrStatus   ErrorClass = "status"
	ErrKeyword  ErrorClass = "keyword"
	ErrBlocked  ErrorClass = "blocked"
	ErrPingLoss ErrorClass = "ping_loss"
)

// CheckResult is one check.
type CheckResult struct {
	At         time.Time
	Outcome    CheckOutcome
	Class      ErrorClass    // set when Outcome is down
	Latency    time.Duration // total; ping: average round trip
	TTFB       time.Duration // http: time to the first response byte
	StatusCode int           // http: the final status
	Loss       int           // ping: percent of echoes lost
	CertExpiry *time.Time    // https: earliest NotAfter in the verified chain
}

// MonitorState is what the scheduler and the dashboard read: the current
// status and what the next check needs to know.
type MonitorState struct {
	Monitor      int64
	Status       MonitorStatus
	Since        time.Time // when Status was entered (down: the first failed check)
	LastCheck    time.Time // zero: never checked
	Failures     int       // consecutive failed checks, confirmations included
	FirstFailure time.Time // the first of them
	Cert         CertStatus
	CertExpiry   *time.Time
	// Revision changes on every write. A check stores its result only if
	// the revision it started from is still current: a pause, resume or
	// edit made while it ran wins over the result.
	Revision int64
}

// MonitorEventKind is a notifiable transition.
type MonitorEventKind string

const (
	EventDown         MonitorEventKind = "down"
	EventUp           MonitorEventKind = "up"
	EventCertExpiring MonitorEventKind = "cert_expiring"
	EventCertRenewed  MonitorEventKind = "cert_renewed"
)

// MonitorEvent is one recorded transition and its notification outcome.
type MonitorEvent struct {
	ID         int64
	Monitor    int64
	Kind       MonitorEventKind
	At         time.Time
	Class      ErrorClass    // down: why
	DownFor    time.Duration // up: how long the incident lasted
	CertExpiry *time.Time    // certificate events
	Notify     bool
	Outcome    string
	Delivery   string
}

// Incident is one down period. End is nil while it is open.
type Incident struct {
	ID       int64
	Monitor  int64
	Start    time.Time
	End      *time.Time
	Class    ErrorClass // the first failure's class
	Failures int        // failed checks during the incident
}

// Span is a half-open interval [Start, End).
type Span struct{ Start, End time.Time }

// MonitorStep is what one result does: the next state, the time it accounts
// for, and the incident and event changes, all stored in one transaction.
type MonitorStep struct {
	Next MonitorState
	// Covered is the span since the previous check that this result
	// accounts for; zero when the gap is too long to vouch for (the server
	// was not checking) or the outcome is unknown.
	Covered     Span
	CoveredDown bool
	// Reclassify is time already counted as up that turned out to be down:
	// the span from the first failed check to its confirmation.
	Reclassify *Span
	Open       *Incident  // an incident starts
	Close      *time.Time // the open incident ends
	Failure    bool       // the open incident gains a failed check
	Events     []MonitorEvent
}

// coverGap is the longest gap between checks that a result vouches for.
// Beyond it the server was not checking (stopped, overloaded), and that
// time counts as neither up nor down.
func coverGap(m Monitor) time.Duration { return 2 * m.Interval }

// StepMonitor applies one result to the state. It never decides
// notifications: silences and channels are the use case's concern.
func StepMonitor(m Monitor, prev MonitorState, r CheckResult) MonitorStep {
	st := MonitorStep{Next: prev}
	st.Next.Monitor = m.ID
	if r.Outcome == CheckUnknown {
		st.Next.LastCheck = r.At
		return st
	}
	gap := r.At.Sub(prev.LastCheck)
	if !prev.LastCheck.IsZero() && gap > 0 && gap <= coverGap(m) && (prev.Status == MonitorUp || prev.Status == MonitorDown) {
		st.Covered = Span{prev.LastCheck, r.At}
		st.CoveredDown = prev.Status == MonitorDown
	}
	st.Next.LastCheck = r.At
	if r.Outcome == CheckUp {
		stepUp(&st, prev, r)
	} else {
		stepDown(&st, m, prev, r)
	}
	stepCert(&st, m, prev, r)
	return st
}

func stepUp(st *MonitorStep, prev MonitorState, r CheckResult) {
	st.Next.Failures, st.Next.FirstFailure = 0, time.Time{}
	if prev.Status == MonitorUp {
		return
	}
	if prev.Status == MonitorDown {
		at := r.At
		st.Close = &at
		st.Events = append(st.Events, MonitorEvent{Kind: EventUp, At: r.At, DownFor: r.At.Sub(prev.Since)})
	}
	st.Next.Status, st.Next.Since = MonitorUp, r.At
}

func stepDown(st *MonitorStep, m Monitor, prev MonitorState, r CheckResult) {
	if prev.Status == MonitorDown {
		st.Failure = true
		return
	}
	st.Next.Failures = prev.Failures + 1
	if prev.Failures == 0 {
		st.Next.FirstFailure = r.At
	}
	if st.Next.Failures <= m.ConfirmRetries {
		return // not confirmed yet: the scheduler re-checks soon
	}
	start := st.Next.FirstFailure
	if prev.Status == MonitorUp && start.Before(r.At) {
		st.Reclassify = &Span{start, r.At}
	}
	st.Next.Status, st.Next.Since = MonitorDown, start
	st.Open = &Incident{Monitor: m.ID, Start: start, Class: r.Class, Failures: st.Next.Failures}
	st.Events = append(st.Events, MonitorEvent{Kind: EventDown, At: r.At, Class: r.Class})
}

func stepCert(st *MonitorStep, m Monitor, prev MonitorState, r CheckResult) {
	if r.CertExpiry == nil {
		return
	}
	exp := *r.CertExpiry
	st.Next.CertExpiry = &exp
	if m.CertWarnDays <= 0 {
		st.Next.Cert = CertUnknown
		return
	}
	expiring := exp.Sub(r.At) <= time.Duration(m.CertWarnDays)*24*time.Hour
	switch {
	case expiring && prev.Cert != CertExpiring:
		st.Next.Cert = CertExpiring
		st.Events = append(st.Events, MonitorEvent{Kind: EventCertExpiring, At: r.At, CertExpiry: &exp})
	case !expiring && prev.Cert == CertExpiring:
		st.Next.Cert = CertOK
		st.Events = append(st.Events, MonitorEvent{Kind: EventCertRenewed, At: r.At, CertExpiry: &exp})
	case !expiring:
		st.Next.Cert = CertOK
	}
}

// NeedsRetry reports whether the state is mid-confirmation: a failed check
// that has not yet opened an incident.
func (s MonitorState) NeedsRetry() bool {
	return s.Failures > 0 && s.Status != MonitorDown && s.Status != MonitorPaused
}
