// Package ingest is the ingest bounded context: authenticating agents and
// turning a batch into stored samples exactly once.
package ingest

import (
	"context"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// AuthHost is who a credential belongs to. Tenant and host come from here and
// never from the request body (PLAN §4.1).
type AuthHost struct {
	Tenant   domain.TenantID
	Host     domain.HostID
	Interval time.Duration
	Enabled  bool
}

// CredentialStore resolves a credential hash to its host. Unscoped by
// necessity: the credential is how the tenant is found.
type CredentialStore interface {
	AuthenticateCredential(ctx context.Context, credHash []byte) (AuthHost, error)
}

// AgentStats is the agent's self-report, stored on the host row.
type AgentStats struct {
	Version      string
	BufferDepth  int
	DroppedTotal uint64
	ClockSkew    time.Duration
}

// Prepared is everything one batch writes. It is built inside the store's
// transaction from the counter state that transaction read, so the counter
// state it carries and the rows it describes commit together or not at all
// (R1 by construction).
type Prepared struct {
	Samples    []domain.CoreSample // sorted by TS, snapped, counters reset-adjusted
	Details    []domain.DetailSample
	Processes  []domain.ProcessSnapshot
	Logs       []domain.LogLine // redacted, within today's budget
	LogStats   LogStats
	Counters   CounterState // state after this batch (core and detail counters)
	Rejected   int          // samples refused individually
	Interval   time.Duration
	Agent      AgentStats
	ReceivedAt time.Time
	// SeqRetention is how long the store keeps this host's batch_seq records.
	// Derived from the ingest window, never configured separately: a pruned
	// seq must be one whose samples the window would reject anyway.
	SeqRetention time.Duration
}

// TxState is what the store reads inside the write transaction for build:
// the host's counter state and the tenant's log budget left today. Reading
// both in the transaction that commits the result is what keeps counters
// and the log quota exact under concurrency and replay (R1; OLD C-M1).
type TxState struct {
	Counters CounterState
	// LogBudget is bytes the tenant may still store today; negative means
	// over budget already.
	LogBudget int64
}

// LogStats are the per-batch log counts the store meters with the batch —
// including for a batch where every line was dropped or shed (OLD C-M11).
type LogStats struct {
	Received   int
	Redacted   int
	Dropped    int    // by a drop rule
	Shed       int    // refused: quota or disk
	ShedReason string // "quota" | "disk"
	Bytes      int64  // billable bytes of the stored lines
}

// BuildFunc computes a Prepared from the transaction's state. A store calls
// it at most once per CommitBatch, inside the write transaction, and never
// for a replayed batch.
type BuildFunc func(state TxState) (Prepared, error)

// CommitResult reports what a CommitBatch did.
type CommitResult struct {
	Replayed bool // the (host, seq) was already stored; nothing was applied
	Stored   int  // samples written
	// Conflicts counts samples whose (host, ts) already existed: counted,
	// never silently ignored (PLAN §4).
	Conflicts int
	// SeriesRefused counts detail samples dropped because they would have
	// created a series past the tenant's ceiling. Existing series are never
	// refused, and hitting the ceiling never fails the batch (OLD A-C1).
	SeriesRefused int
	LogsStored    int
}

// Publisher receives samples after they are durably stored — never before
// (R1: a live chart must not show a point the database then lost). It must
// not block: the ingest path waits on it.
type Publisher interface {
	PublishCore(tenant domain.TenantID, host domain.HostID, samples []domain.CoreSample)
	PublishLogs(tenant domain.TenantID, host domain.HostID, lines []domain.LogLine)
}

// Redactor removes secrets from log lines before storage. It returns the
// lines to keep, how many it changed, and how many drop rules removed.
type Redactor interface {
	Redact(ctx context.Context, s domain.Scope, lines []domain.LogLine) ([]domain.LogLine, int, int, error)
}

// DiskGuard reports whether the data directory has room for logs. Below the
// watermark logs are shed and metrics keep flowing.
type DiskGuard interface {
	LogsAllowed() bool
}

// Store persists a batch as one unit of work: raw rows, latest values,
// counter state, host liveness and the batch_seq record, in one transaction.
// An adapter that cannot do this atomically must document which half can
// land without the other (PLAN §4).
type Store interface {
	CommitBatch(ctx context.Context, s domain.Scope, host domain.HostID, seq uint64, build BuildFunc) (CommitResult, error)
}
