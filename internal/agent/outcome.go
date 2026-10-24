package agent

import "net/http"

// Outcome is what the agent does with a batch after one send attempt. It is
// the agent half of the response contract (PLAN §2): every status has
// exactly one meaning, and the defaults lean towards keeping data.
type Outcome int

const (
	// Acked: stored, or stored before (replay). Delete it.
	Acked Outcome = iota
	// Dropped: permanently invalid. Delete it, count it, never resend —
	// resending would replay the same refused head forever (OLD C-Minor).
	Dropped
	// Stopped: credential unknown or host disabled. Keep buffering, stop
	// sending, log loudly; only a re-enrolment or re-enable fixes it.
	Stopped
	// Retry: nothing was stored. Back off with jitter, resend the same batch.
	Retry
)

func (o Outcome) String() string {
	return [...]string{"acked", "dropped", "stopped", "retry"}[o]
}

// classify maps an HTTP status (0 = transport error) to an outcome.
// Unknown statuses retry: wrongly retrying is recoverable, wrongly dropping
// is data loss.
func classify(status int) Outcome {
	switch status {
	case http.StatusOK:
		return Acked
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return Dropped
	case http.StatusUnauthorized, http.StatusForbidden:
		return Stopped
	default:
		return Retry
	}
}
