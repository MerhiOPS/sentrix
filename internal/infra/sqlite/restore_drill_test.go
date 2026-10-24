package sqlite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

func restored(t *testing.T, st *Store) *Store {
	t.Helper()
	ctx := context.Background()
	ops, _ := domain.NewOpsScope("test")
	backup := filepath.Join(t.TempDir(), "backup.db")
	if err := st.Backup(ctx, ops, backup); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := Restore(ctx, backup, dir, "sentrix.db"); err != nil {
		t.Fatal(err)
	}
	if err := Restore(ctx, backup, dir, "sentrix.db"); err == nil {
		t.Fatal("restore over an existing database was allowed")
	}
	db, err := OpenMigrated(ctx, filepath.Join(dir, "sentrix.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeDB(t, db) })
	return NewStore(db)
}

// TestRestoreReproducesDatasetExactly: Phase 7 acceptance — every table,
// search index included, has the same rows after backup and restore.
func TestRestoreReproducesDatasetExactly(t *testing.T) {
	w := newEraseWorld(t)
	want, err := w.st.TableDigest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, err := restored(t, w.st).TableDigest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(want) < 20 {
		t.Fatalf("only %d tables digested", len(want))
	}
	for table, d := range want {
		if got[table] != d {
			t.Errorf("table %s: restored %s, source %s", table, got[table], d)
		}
	}
	if !strings.HasPrefix(want["metrics_core"], "80:") || !strings.HasPrefix(want["logs_fts_docsize"], "80:") {
		t.Fatalf("seed did not produce the expected dataset: %v", want)
	}
}

// TestBackupUnderConcurrentWritesIsCrossTableConsistent: Phase 7
// acceptance. A writer commits marker pairs — a sample and a log line with
// the same number, in one batch — while the backup runs. Every restored
// marker must have its pair: a per-table copy would cut between them.
func TestBackupUnderConcurrentWritesIsCrossTableConsistent(t *testing.T) {
	w := newEraseWorld(t)
	ctx := context.Background()
	sc, _ := domain.NewScope(w.tenant[0])
	host := w.hosts[0][0]
	base := time.Now().Add(-48 * time.Hour).Truncate(time.Minute)
	var written atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := int64(1); ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			at := base.Add(time.Duration(i) * 5 * time.Second)
			_, err := w.st.CommitBatch(ctx, sc, host, uint64(1000+i), func(st ingest.TxState) (ingest.Prepared, error) {
				return ingest.Prepared{Counters: st.Counters, Interval: 5 * time.Second, ReceivedAt: time.Now(), SeqRetention: 72 * time.Hour,
					Samples: []domain.CoreSample{{TS: at, CPUPercent: 1}},
					Logs:    []domain.LogLine{{TS: at, Source: "marker", Level: domain.LevelInfo, Message: fmt.Sprintf("marker-%d", i)}}}, nil
			})
			if err != nil {
				t.Error(err)
				return
			}
			written.Store(i)
		}
	}()
	for written.Load() < 50 {
		time.Sleep(time.Millisecond)
	}
	r := restored(t, w.st) // the backup runs while markers keep landing
	close(stop)
	wg.Wait()

	samples, err := r.RawCore(ctx, sc, host, base, base.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	clauses, _ := query.ParseSearch("marker")
	var logs []query.LogEntry
	q := query.LogQuery{Clauses: clauses, From: base, To: base.Add(24 * time.Hour), Limit: query.MaxLogLimit}
	for {
		page, err := r.SearchLogs(ctx, sc, q)
		if err != nil {
			t.Fatal(err)
		}
		logs = append(logs, page...)
		if len(page) < q.Limit {
			break
		}
		last := page[len(page)-1]
		q.Before = &query.LogCursor{TS: last.Line.TS, ID: last.ID}
	}
	inMetrics := map[int64]bool{}
	for _, s := range samples {
		inMetrics[s.TS.Sub(base).Milliseconds()/5000] = true
	}
	if len(samples) < 50 || len(samples) != len(logs) {
		t.Fatalf("restored %d marker samples and %d marker lines (writer reached %d)", len(samples), len(logs), written.Load())
	}
	for _, l := range logs {
		var i int64
		_, _ = fmt.Sscanf(l.Line.Message, "marker-%d", &i)
		if !inMetrics[i] {
			t.Fatalf("marker %d is in logs but not in metrics: the backup is not one snapshot", i)
		}
	}
	t.Logf("backup taken with %d of %d marker batches; all pairs intact", len(samples), written.Load())
}

// TestRestoreDrillTimed measures backup and restore on a larger dataset
// (SENTRIX_DRILL=1; the figures are in docs/restore-drill.md).
func TestRestoreDrillTimed(t *testing.T) {
	if os.Getenv("SENTRIX_DRILL") == "" {
		t.Skip("set SENTRIX_DRILL=1 to run the timed restore drill")
	}
	st := newMigratedStore(t)
	ctx := context.Background()
	nHosts, nBatches := envInt("SENTRIX_DRILL_HOSTS", 20), envInt("SENTRIX_DRILL_BATCHES", 360)
	hosts := seedDrillHosts(t, st, nHosts)
	start := time.Now().Add(-48 * time.Hour).Truncate(time.Hour)
	seedStart := time.Now()
	for i, h := range hosts {
		for b := 0; b < nBatches; b++ { // 10 s batches, 2 samples + 36 details + 5 log lines each
			at := start.Add(time.Duration(b) * 10 * time.Second)
			_, err := st.CommitBatch(ctx, h.Scope, h.Host, uint64(b+1), drillBatch(at, i))
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Logf("seeded in %v", time.Since(seedStart).Round(time.Millisecond))
	src, _ := os.Stat(st.db.Path())
	ops, _ := domain.NewOpsScope("drill")
	backup := filepath.Join(t.TempDir(), "backup.db")
	t0 := time.Now()
	if err := st.Backup(ctx, ops, backup); err != nil {
		t.Fatal(err)
	}
	backupTook := time.Since(t0)
	b, _ := os.Stat(backup)
	t1 := time.Now()
	dir := t.TempDir()
	if err := Restore(ctx, backup, dir, "sentrix.db"); err != nil {
		t.Fatal(err)
	}
	restoreTook := time.Since(t1)
	want, _ := st.TableDigest(ctx)
	db, err := OpenMigrated(ctx, filepath.Join(dir, "sentrix.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB(t, db)
	got, _ := NewStore(db).TableDigest(ctx)
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("table %s differs after the timed restore", k)
		}
	}
	t.Logf("database %.1f MiB, backup %.1f MiB; backup %v, restore (copy + verify + integrity_check) %v; metrics_core %s, logs %s, samples_detail %s",
		float64(src.Size())/(1<<20), float64(b.Size())/(1<<20), backupTook.Round(time.Millisecond), restoreTook.Round(time.Millisecond),
		want["metrics_core"], want["logs"], want["samples_detail"])
}

// A backup that opens but is damaged is refused, and nothing is left in the
// data directory.
func TestRestoreRefusesCorruptBackup(t *testing.T) {
	w := newEraseWorld(t)
	ops, _ := domain.NewOpsScope("test")
	backup := filepath.Join(t.TempDir(), "backup.db")
	if err := w.st.Backup(context.Background(), ops, backup); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	page := 4096
	for p := len(data)/page - 3; p < len(data)/page; p++ { // damage the last data pages, keep the schema page
		for i := p*page + 100; i < p*page+400; i++ {
			data[i] ^= 0x5a
		}
	}
	if err := os.WriteFile(backup, data, 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := Restore(context.Background(), backup, dir, "sentrix.db"); err == nil {
		t.Fatal("a damaged backup was restored")
	}
	if _, err := os.Stat(filepath.Join(dir, "sentrix.db")); err == nil {
		t.Fatal("the refused restore left a database behind")
	}
}
