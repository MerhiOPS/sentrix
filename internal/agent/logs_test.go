package agent

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/logtail"
)

func TestSealSplitsLogsMetricsFirstMarksLast(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	seq, _ := OpenSeqSource(t.TempDir(), fixedClock(now))
	b := NewBatcher(seq, 5*time.Second, zeroStats{})
	b.Add(domain.CoreSample{TS: now})
	var lines []domain.LogLine
	for i := 0; i < maxLogsPerBatch*2+10; i++ {
		lines = append(lines, domain.LogLine{TS: now, Source: "app", Level: domain.LevelInfo, Message: fmt.Sprint(i)})
	}
	b.AddLogs("files", lines, "mark-after-all")
	ps, err := b.Seal()
	if err != nil || len(ps) != 3 {
		t.Fatalf("%d batches, %v; want 3", len(ps), err)
	}
	if ps[0].Samples != 1 || ps[1].Samples != 0 || ps[0].Marks != nil || ps[1].Marks != nil || ps[2].Marks["files"] != "mark-after-all" {
		t.Fatalf("metrics must ride first and the mark last: %+v", []map[string]logtail.Mark{ps[0].Marks, ps[1].Marks, ps[2].Marks})
	}
	total := 0
	for _, p := range ps {
		wb, _ := gunzipBatch(p.Body)
		total += len(wb.Logs)
		if len(wb.Logs) > maxLogsPerBatch {
			t.Fatalf("batch with %d logs, max %d", len(wb.Logs), maxLogsPerBatch)
		}
	}
	if total != len(lines) {
		t.Fatalf("%d lines across batches, want %d", total, len(lines))
	}
}

// recordingSource is a log source whose Commit is observable.
type recordingSource struct {
	committed []logtail.Mark
}

func (r *recordingSource) Name() string { return "rec" }
func (r *recordingSource) Poll(context.Context) ([]domain.LogLine, logtail.Mark, error) {
	return nil, nil, nil
}
func (r *recordingSource) Commit(m logtail.Mark) ([]byte, error) {
	r.committed = append(r.committed, m)
	return []byte(fmt.Sprint(m)), nil
}
func (r *recordingSource) Restore([]byte) error { return nil }
func (r *recordingSource) Close() error         { return nil }

// TestPositionsAdvanceOnlyWhenBatchSettles: R1 — a sent-but-unacknowledged
// batch must not move the position; acked and permanently refused do.
func TestPositionsAdvanceOnlyWhenBatchSettles(t *testing.T) {
	for _, c := range []struct {
		status int
		commit bool
	}{{http.StatusOK, true}, {http.StatusUnprocessableEntity, true}, {http.StatusServiceUnavailable, false}} {
		s, buf, _ := newTestShipper(t, c.status)
		dir := t.TempDir()
		src := &recordingSource{}
		s.settled = func(p Pending) {
			_ = commitPositions(dir, map[string]logtail.Source{"rec": src}, p.Marks)
		}
		p := pending(t, 1)
		p.Marks = map[string]logtail.Mark{"rec": "pos-1"}
		buf.Push(p)
		s.handle(context.Background(), p)
		_, err := os.Stat(positionFile(dir, "rec"))
		if (len(src.committed) == 1) != c.commit || (err == nil) != c.commit {
			t.Fatalf("status %d: committed %v, file exists %v; want commit=%v", c.status, src.committed, err == nil, c.commit)
		}
	}
}

func TestAgentShipsFileLinesAndPersistsPositionAfterAck(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "app.log")
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := logtail.NewFileSource("files", []string{logPath})
	if err != nil {
		t.Fatal(err)
	}
	s, _, srv := newTestShipper(t, http.StatusOK)
	cfg := DefaultConfig(t.TempDir())
	cfg.Interval, cfg.BatchEvery = time.Second, time.Second
	a, err := New(cfg, Identity{ServerURL: strings.TrimSuffix(s.endpoint, "/ingest/v1/batch"), Credential: "x"},
		Sources{Core: fakeCore{}, Logs: []logtail.Source{src}}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3500*time.Millisecond)
	defer cancel()
	go func() {
		time.Sleep(1200 * time.Millisecond)
		f, _ := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
		_, _ = f.WriteString("hello from the log\n")
		_ = f.Close()
	}()
	a.Run(ctx)
	if len(srv.seqs) == 0 {
		t.Fatal("nothing shipped")
	}
	state, err := os.ReadFile(positionFile(cfg.StateDir, "files"))
	if err != nil || !strings.Contains(string(state), `"offset":19`) {
		t.Fatalf("position after ack: %s %v (want offset 19)", state, err)
	}
}

type fakeCore struct{}

func (fakeCore) Collect(context.Context) (domain.CoreSample, []error) {
	return domain.CoreSample{TS: time.Now()}, nil
}
