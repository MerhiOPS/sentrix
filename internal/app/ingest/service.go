package ingest

import (
	"context"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Batch is a decoded batch, already mapped from the wire format.
type Batch struct {
	Seq       uint64
	Interval  time.Duration // as reported by the agent; zero = use the host's
	Samples   []domain.CoreSample
	Details   []domain.DetailSample
	Processes []domain.ProcessSnapshot
	Agent     AgentStats
}

// Per-batch caps (OLD A-M5). The transport enforces the same numbers; the
// use case does not trust that it did.
const (
	MaxSamples   = 1000
	MaxDetails   = 20000
	MaxSnapshots = 200
)

// Result is what the agent is told.
type Result struct {
	Replayed      bool
	Stored        int
	Rejected      int
	Conflicts     int
	SeriesRefused int
	ServerNow     time.Time
}

// Service ingests batches.
type Service struct {
	store  Store
	window Window
	now    func() time.Time
	pub    Publisher // optional
}

// NewService builds the ingest use case.
func NewService(store Store, window Window, now func() time.Time) *Service {
	return &Service{store: store, window: window, now: now}
}

// SetPublisher attaches the live feed. Call before serving.
func (s *Service) SetPublisher(p Publisher) { s.pub = p }

// Ingest stores one batch for an authenticated host. The scope is built from
// the credential's tenant; nothing in b can change which tenant or host the
// rows land under.
func (s *Service) Ingest(ctx context.Context, ah AuthHost, b Batch) (Result, error) {
	if !ah.Enabled {
		return Result{}, fmt.Errorf("%w: host is disabled", domain.ErrForbidden)
	}
	if b.Seq == 0 || b.Seq > domain.MaxCounter {
		return Result{}, fmt.Errorf("%w: batch_seq must be in [1, 2^63)", domain.ErrInvalid)
	}
	if len(b.Samples) > MaxSamples || len(b.Details) > MaxDetails || len(b.Processes) > MaxSnapshots {
		return Result{}, fmt.Errorf("%w: batch over size limits", domain.ErrInvalid)
	}
	scope, err := domain.NewScope(ah.Tenant)
	if err != nil {
		return Result{}, err
	}
	interval := b.Interval
	if domain.ValidateInterval(interval) != nil {
		interval = ah.Interval
	}
	now := s.now().UTC()
	samples, rejected := normalize(b.Samples, interval, s.window, now)
	details, rejDetails := normalizeDetails(b.Details, interval, s.window, now)
	snaps, rejSnaps := normalizeSnapshots(b.Processes, s.window, now)
	rejected += rejDetails + rejSnaps

	var stored []domain.CoreSample
	build := func(state CounterState) (Prepared, error) {
		folded, next := foldCounters(state, samples)
		foldedDetails, next := foldDetailCounters(next, details)
		stored = folded
		return Prepared{
			Samples: folded, Details: foldedDetails, Processes: snaps, Counters: next, Rejected: rejected,
			Interval: interval, Agent: b.Agent, ReceivedAt: now,
			SeqRetention: s.window.SeqRetention(),
		}, nil
	}
	cr, err := s.store.CommitBatch(ctx, scope, ah.Host, b.Seq, build)
	if err != nil {
		return Result{}, fmt.Errorf("commit batch: %w", err)
	}
	if s.pub != nil && !cr.Replayed && len(stored) > 0 {
		s.pub.PublishCore(ah.Tenant, ah.Host, stored) // after commit, never before
	}
	res := Result{Replayed: cr.Replayed, Stored: cr.Stored, Conflicts: cr.Conflicts,
		SeriesRefused: cr.SeriesRefused, ServerNow: now}
	if !cr.Replayed {
		res.Rejected = rejected
	}
	return res, nil
}
