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
	Counters   CounterState        // state after this batch
	Rejected   int                 // samples refused individually
	Interval   time.Duration
	Agent      AgentStats
	ReceivedAt time.Time
	// SeqRetention is how long the store keeps this host's batch_seq records.
	// Derived from the ingest window, never configured separately: a pruned
	// seq must be one whose samples the window would reject anyway.
	SeqRetention time.Duration
}

// BuildFunc computes a Prepared from the stored counter state. A store calls
// it at most once per CommitBatch, inside the write transaction, and never
// for a replayed batch.
type BuildFunc func(state CounterState) (Prepared, error)

// CommitResult reports what a CommitBatch did.
type CommitResult struct {
	Replayed bool // the (host, seq) was already stored; nothing was applied
	Stored   int  // samples written
	// Conflicts counts samples whose (host, ts) already existed: counted,
	// never silently ignored (PLAN §4).
	Conflicts int
}

// Store persists a batch as one unit of work: raw rows, latest values,
// counter state, host liveness and the batch_seq record, in one transaction.
// An adapter that cannot do this atomically must document which half can
// land without the other (PLAN §4).
type Store interface {
	CommitBatch(ctx context.Context, s domain.Scope, host domain.HostID, seq uint64, build BuildFunc) (CommitResult, error)
}
