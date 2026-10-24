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
	StateDir   string
	Interval   time.Duration // sampling interval
	BatchEvery time.Duration // how often samples are sealed into a batch
	BufferMax  int           // batches held while the server is unreachable
}

// DefaultConfig is 5 s samples in 10 s batches, holding an hour of backlog.
func DefaultConfig(stateDir string) Config {
	return Config{StateDir: stateDir, Interval: 5 * time.Second, BatchEvery: 10 * time.Second, BufferMax: 360}
}

// Collector takes one sample.
type Collector interface {
	Collect(ctx context.Context) (domain.CoreSample, []error)
}

// Agent runs collection and shipping.
type Agent struct {
	cfg     Config
	col     Collector
	batcher *Batcher
	buf     *Buffer
	ship    *Shipper
	log     *slog.Logger
}

// New wires an agent for an enrolled identity.
func New(cfg Config, id Identity, col Collector, log *slog.Logger) (*Agent, error) {
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
	a := &Agent{cfg: cfg, col: col, buf: NewBuffer(cfg.BufferMax), log: log}
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
	defer sample.Stop()
	defer seal.Stop()
	// Prime the CPU counters and discard the result: the first reading is
	// utilisation averaged since boot, not over the last interval.
	_, _ = a.col.Collect(ctx)
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			a.seal()
			a.drain()
			return
		case <-sample.C:
			a.collect(ctx)
		case <-seal.C:
			a.seal()
		}
	}
}

func (a *Agent) collect(ctx context.Context) {
	s, errs := a.col.Collect(ctx)
	for _, e := range errs {
		a.log.Debug("collector", "err", e)
	}
	a.batcher.Add(s)
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
