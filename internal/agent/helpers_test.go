package agent

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/wire"
)

func gunzipBatch(b []byte) (wire.Batch, error) {
	var out wire.Batch
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return out, err
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(raw, &out)
}

type zeroStats struct{}

func (zeroStats) BufferDepth() int         { return 3 }
func (zeroStats) DroppedTotal() uint64     { return 4 }
func (zeroStats) ClockSkew() time.Duration { return 1500 * time.Millisecond }

func TestBatcherSealsWithPersistedSeqAndStats(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	seq, _ := OpenSeqSource(t.TempDir(), fixedClock(now))
	b := NewBatcher(seq, 5*time.Second, zeroStats{})
	if _, ok, _ := b.Seal(); ok {
		t.Fatal("empty batcher sealed a batch")
	}
	b.Add(domain.CoreSample{TS: now, CPUPercent: 3, NetRxBytes: 9})
	b.Add(domain.CoreSample{TS: now.Add(5 * time.Second)})
	p, ok, err := b.Seal()
	if err != nil || !ok || p.Samples != 2 {
		t.Fatalf("seal: %+v %v %v", p, ok, err)
	}
	wb, err := gunzipBatch(p.Body)
	if err != nil {
		t.Fatal(err)
	}
	if wb.BatchSeq != p.Seq || wb.IntervalMS != 5000 || len(wb.Samples) != 2 || wb.Samples[0].NetRxBytes != 9 {
		t.Fatalf("batch body = %+v", wb)
	}
	if wb.Agent.BufferDepth != 3 || wb.Agent.DroppedTotal != 4 || wb.Agent.ClockSkewMS != 1500 {
		t.Fatalf("agent stats = %+v", wb.Agent)
	}
	if _, ok, _ := b.Seal(); ok {
		t.Fatal("samples were sealed twice")
	}
}
