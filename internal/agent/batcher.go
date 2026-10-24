package agent

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/logtail"
	"github.com/MerhiOPS/sentrix/internal/wire"
)

// Stats is the agent's self-report shipped with every batch.
type Stats interface {
	BufferDepth() int
	DroppedTotal() uint64
	ClockSkew() time.Duration
}

// Batcher accumulates samples and seals them into encoded batches.
type Batcher struct {
	mu       sync.Mutex
	samples  []wire.Sample
	details  []wire.Detail
	snaps    []wire.Snapshot
	logs     []wire.LogLine
	marks    map[string]logtail.Mark
	seq      *SeqSource
	interval time.Duration
	stats    Stats
}

// NewBatcher builds a batcher drawing sequence numbers from seq.
func NewBatcher(seq *SeqSource, interval time.Duration, stats Stats) *Batcher {
	return &Batcher{seq: seq, interval: interval, stats: stats}
}

// Add records one sample.
func (b *Batcher) Add(s domain.CoreSample) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.samples = append(b.samples, toWire(s))
}

// AddDetails records one round of detail samples.
func (b *Batcher) AddDetails(ds []domain.DetailSample) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, d := range ds {
		b.details = append(b.details, wire.Detail{TSMS: d.TS.UnixMilli(), Metric: d.Metric,
			Kind: string(d.Kind), Labels: d.Labels, Value: d.Value})
	}
}

// AddSnapshot records one process snapshot.
func (b *Batcher) AddSnapshot(s domain.ProcessSnapshot) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ws := wire.Snapshot{TSMS: s.TS.UnixMilli(), Procs: make([]wire.Process, len(s.Procs))}
	for i, p := range s.Procs {
		ws.Procs[i] = wire.Process{Name: p.Name, CPU: p.CPUPercent, RSS: p.RSS}
	}
	b.snaps = append(b.snaps, ws)
}

// Seal turns everything accumulated into Pending batches: usually one, more
// when logs exceed a batch's limits. Seqs are allocated (and persisted)
// before the batches exist, so a crash after Seal can never reuse one.
func (b *Batcher) Seal() ([]Pending, error) {
	b.mu.Lock()
	c := contents{samples: b.samples, details: b.details, snaps: b.snaps, logs: b.logs, marks: b.marks}
	b.samples, b.details, b.snaps, b.logs, b.marks = nil, nil, nil, nil, nil
	b.mu.Unlock()
	if c.empty() {
		return nil, nil
	}
	chunks := c.split()
	out := make([]Pending, 0, len(chunks))
	for _, ch := range chunks {
		p, err := b.encode(ch)
		if err != nil {
			b.restore(c)
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (b *Batcher) encode(c contents) (Pending, error) {
	seq, err := b.seq.Next()
	if err != nil {
		return Pending{}, err
	}
	body, err := encodeBatch(wire.Batch{
		BatchSeq: seq, IntervalMS: b.interval.Milliseconds(), Samples: c.samples, Details: c.details,
		Processes: c.snaps, Logs: c.logs,
		Agent: wire.AgentStats{
			Version: Version, BufferDepth: b.stats.BufferDepth(),
			DroppedTotal: b.stats.DroppedTotal(), ClockSkewMS: b.stats.ClockSkew().Milliseconds(),
		},
	})
	if err != nil {
		return Pending{}, err
	}
	return Pending{Seq: seq, Body: body, Samples: len(c.samples), Marks: c.marks}, nil
}

// restore puts everything back after a failed seal, ahead of newer data.
// Marks already superseded by newer ones are kept as the newer ones.
func (b *Batcher) restore(c contents) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.samples = append(c.samples, b.samples...)
	b.details = append(c.details, b.details...)
	b.snaps = append(c.snaps, b.snaps...)
	b.logs = append(c.logs, b.logs...)
	if b.marks == nil {
		b.marks = c.marks
	}
}

func encodeBatch(b wire.Batch) ([]byte, error) {
	raw, err := json.Marshal(b)
	if err != nil {
		return nil, fmt.Errorf("encode batch: %w", err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return nil, fmt.Errorf("compress batch: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("compress batch: %w", err)
	}
	return buf.Bytes(), nil
}

func toWire(s domain.CoreSample) wire.Sample {
	return wire.Sample{
		TSMS: s.TS.UnixMilli(), CPUPercent: s.CPUPercent, MemoryPercent: s.MemoryPercent,
		DiskPercent: s.DiskPercent, Load1: s.Load1, Load5: s.Load5, Load15: s.Load15,
		MemoryUsed: s.MemoryUsed, UptimeSeconds: s.UptimeSeconds,
		NetRxBytes: s.NetRxBytes, NetTxBytes: s.NetTxBytes,
		DiskReadBytes: s.DiskReadBytes, DiskWriteBytes: s.DiskWriteBytes,
	}
}
