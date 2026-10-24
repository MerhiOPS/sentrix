package agent

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/logtail"
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
	Logs   []logtail.Source
}

// logPollEvery is how often log sources are read.
const logPollEvery = time.Second

// Agent runs collection and shipping.
type Agent struct {
	cfg     Config
	src     Sources
	batcher *Batcher
	buf     *Buffer
	ship    *Shipper
	logSrc  map[string]logtail.Source
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
	a.logSrc = map[string]logtail.Source{}
	for _, s := range src.Logs {
		a.logSrc[s.Name()] = s
	}
	if err := restorePositions(cfg.StateDir, src.Logs); err != nil {
		return nil, err
	}
	a.ship.settled = a.settle
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
	logs := time.NewTicker(logPollEvery)
	defer logs.Stop()
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
		case <-logs.C:
			a.pollLogs(ctx)
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
	ps, err := a.batcher.Seal()
	if err != nil {
		a.log.Error("seal batch", "err", err)
		return
	}
	for _, p := range ps {
		a.buf.Push(p)
	}
}

func (a *Agent) pollLogs(ctx context.Context) {
	for name, src := range a.logSrc {
		lines, mark, err := src.Poll(ctx)
		if err != nil {
			a.log.Debug("log source", "source", name, "err", err)
		}
		if len(lines) > 0 || mark != nil {
			a.batcher.AddLogs(name, lines, mark)
		}
	}
}

// settle commits the log positions of a batch that left the buffer for good.
func (a *Agent) settle(p Pending) {
	if len(p.Marks) == 0 {
		return
	}
	if err := commitPositions(a.cfg.StateDir, a.logSrc, p.Marks); err != nil {
		a.log.Warn("persist log positions", "err", err) // retried with the next settled batch
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
