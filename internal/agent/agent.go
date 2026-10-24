package agent

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Config controls the agent loop.
type Config struct {
	StateDir     string
	Interval     time.Duration // sampling interval
	BatchEvery   time.Duration // how often samples are sealed into a batch
	ProcessEvery time.Duration // how often a top-N process snapshot is taken
	BufferMax    int           // batches held while the server is unreachable
}

// DefaultConfig is 5 s samples in 10 s batches, a process snapshot every
// 30 s, holding an hour of backlog.
func DefaultConfig(stateDir string) Config {
	return Config{StateDir: stateDir, Interval: 5 * time.Second, BatchEvery: 10 * time.Second,
		ProcessEvery: domain.ProcessSnapshotGap, BufferMax: 360}
}

// Collector takes one core sample.
type Collector interface {
	Collect(ctx context.Context) (domain.CoreSample, []error)
}

// DetailCollector takes one round of detail samples.
type DetailCollector interface {
	Collect(ctx context.Context) ([]domain.DetailSample, []error)
}

// ProcessCollector takes one process snapshot.
type ProcessCollector interface {
	Collect(ctx context.Context) (domain.ProcessSnapshot, error)
}

// Sources are the agent's collectors. Detail and Procs are optional.
type Sources struct {
	Core   Collector
	Detail DetailCollector
	Procs  ProcessCollector
}

// Agent runs collection and shipping.
type Agent struct {
	cfg     Config
	src     Sources
	batcher *Batcher
	buf     *Buffer
	ship    *Shipper
	log     *slog.Logger
}

// New wires an agent for an enrolled identity.
func New(cfg Config, id Identity, src Sources, log *slog.Logger) (*Agent, error) {
	pin, err := id.Pin()
	if err != nil {
		return nil, err
	}
	client, err := NewClient(id.ServerURL, pin)
	if err != nil {
		return nil, err
	}
	seq, err := OpenSeqSource(cfg.StateDir, time.Now)
	if err != nil {
		return nil, err
	}
	a := &Agent{cfg: cfg, src: src, buf: NewBuffer(cfg.BufferMax), log: log}
	a.ship = NewShipper(client, id, a.buf, log)
	a.batcher = NewBatcher(seq, cfg.Interval, a)
	return a, nil
}

// drainTimeout bounds the final flush on shutdown, so a stop command is
// never held hostage by an unreachable server.
const drainTimeout = 5 * time.Second

// Run collects and ships until ctx ends. On shutdown it seals what it has
// and gives the shipper drainTimeout to send it; anything still unsent is
// lost until the disk WAL lands in Phase 6.
func (a *Agent) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); a.ship.Run(ctx) }()

	sample := time.NewTicker(a.cfg.Interval)
	seal := time.NewTicker(a.cfg.BatchEvery)
	procs := time.NewTicker(max(a.cfg.ProcessEvery, time.Second))
	defer sample.Stop()
	defer seal.Stop()
	defer procs.Stop()
	a.prime(ctx)
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			a.seal()
			a.drain()
			return
		case <-sample.C:
			a.collect(ctx)
		case <-procs.C:
			a.snapshot(ctx)
		case <-seal.C:
			a.seal()
		}
	}
}

// prime takes and discards first readings: CPU (total, per core, per
// process) is measured since the previous call, and the first call has none.
func (a *Agent) prime(ctx context.Context) {
	_, _ = a.src.Core.Collect(ctx)
	if a.src.Detail != nil {
		_, _ = a.src.Detail.Collect(ctx)
	}
	if a.src.Procs != nil {
		_, _ = a.src.Procs.Collect(ctx)
	}
}

func (a *Agent) collect(ctx context.Context) {
	s, errs := a.src.Core.Collect(ctx)
	if a.src.Detail != nil {
		d, derrs := a.src.Detail.Collect(ctx)
		a.batcher.AddDetails(d)
		errs = append(errs, derrs...)
	}
	for _, e := range errs {
		a.log.Debug("collector", "err", e)
	}
	a.batcher.Add(s)
}

func (a *Agent) snapshot(ctx context.Context) {
	if a.src.Procs == nil {
		return
	}
	snap, err := a.src.Procs.Collect(ctx)
	if err != nil {
		a.log.Debug("process snapshot", "err", err)
		return
	}
	a.batcher.AddSnapshot(snap)
}

func (a *Agent) seal() {
	p, ok, err := a.batcher.Seal()
	if err != nil {
		a.log.Error("seal batch", "err", err)
		return
	}
	if ok {
		a.buf.Push(p)
	}
}

func (a *Agent) drain() {
	ctx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()
	if left := a.ship.Drain(ctx); left > 0 {
		a.log.Warn("shutdown with unsent batches", "batches", left)
	}
}

// BufferDepth, DroppedTotal and ClockSkew implement Stats.
func (a *Agent) BufferDepth() int         { return a.buf.Len() }
func (a *Agent) DroppedTotal() uint64     { return a.buf.Dropped() }
func (a *Agent) ClockSkew() time.Duration { return a.ship.ClockSkew() }
