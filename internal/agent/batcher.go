package agent

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
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

// Seal turns the accumulated samples into a Pending batch. It returns false
// when there is nothing to send. The seq is allocated (and persisted) before
// the batch exists, so a crash after Seal can never reuse it.
func (b *Batcher) Seal() (Pending, bool, error) {
	b.mu.Lock()
	samples, details, snaps := b.samples, b.details, b.snaps
	b.samples, b.details, b.snaps = nil, nil, nil
	b.mu.Unlock()
	if len(samples) == 0 && len(details) == 0 && len(snaps) == 0 {
		return Pending{}, false, nil
	}
	seq, err := b.seq.Next()
	if err != nil {
		b.restore(samples, details, snaps)
		return Pending{}, false, err
	}
	body, err := encodeBatch(wire.Batch{
		BatchSeq: seq, IntervalMS: b.interval.Milliseconds(), Samples: samples, Details: details, Processes: snaps,
		Agent: wire.AgentStats{
			Version: Version, BufferDepth: b.stats.BufferDepth(),
			DroppedTotal: b.stats.DroppedTotal(), ClockSkewMS: b.stats.ClockSkew().Milliseconds(),
		},
	})
	if err != nil {
		return Pending{}, false, err
	}
	return Pending{Seq: seq, Body: body, Samples: len(samples)}, true, nil
}

// restore puts everything back after a failed seal, ahead of newer data.
func (b *Batcher) restore(samples []wire.Sample, details []wire.Detail, snaps []wire.Snapshot) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.samples = append(samples, b.samples...)
	b.details = append(details, b.details...)
	b.snaps = append(snaps, b.snaps...)
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
