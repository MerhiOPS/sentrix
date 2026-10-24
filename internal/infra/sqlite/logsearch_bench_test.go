package sqlite

// Log search benchmark (PLAN Phase 4: "search latency over 1M lines
// measured and published"). One million realistic lines are stored through
// the real CommitBatch path, then each query shape is timed.
//
//	go test ./internal/infra/sqlite -run '^$' -bench LogSearch -benchtime 20x -timeout 30m

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/storetest"
)

const (
	benchLines   = 1_000_000
	searchBudget = 10 * time.Second
)

var templates = []string{
	"GET /api/v1/orders/%d 200 %dms user=[EMAIL]",
	"POST /api/v1/payments/%d 201 %dms",
	"upstream timeout after %dms talking to payments-%d",
	"connection refused by db-%d:5432 retry %d",
	"cache miss for key session:%d (%d bytes)",
	"worker %d finished job %d",
	"disk usage at %d%% on volume %d",
	"TLS handshake error from [IP]: remote error %d (%d)",
}

func benchStore(b *testing.B) (*Store, []storetest.BenchHost) {
	b.Helper()
	dir := b.TempDir()
	db, err := OpenMigrated(context.Background(), filepath.Join(dir, "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	st := NewStore(db)
	hosts := storetest.SeedBenchHosts(b, st, 10)
	rng := rand.New(rand.NewPCG(1, 2))
	t0 := time.Now().Add(-23 * time.Hour)
	start := time.Now()
	const perBatch = 2000
	for i := 0; i < benchLines/perBatch; i++ {
		h := hosts[i%len(hosts)]
		lines := make([]domain.LogLine, perBatch)
		for j := range lines {
			n := i*perBatch + j
			msg := fmt.Sprintf(templates[rng.IntN(len(templates))], rng.IntN(100000), rng.IntN(5000))
			if n%100_000 == 0 {
				msg += " RAREMARKER-" + fmt.Sprint(n)
			}
			lines[j] = domain.LogLine{TS: t0.Add(time.Duration(n) * 80 * time.Millisecond), Source: "/var/log/app.log",
				Level: []domain.Level{domain.LevelInfo, domain.LevelInfo, domain.LevelWarn, domain.LevelError}[n%4], Message: msg}
		}
		_, err := st.CommitBatch(context.Background(), h.Scope, h.Host, uint64(i+1), func(s ingest.TxState) (ingest.Prepared, error) {
			return ingest.Prepared{Logs: lines, LogStats: ingest.LogStats{Received: len(lines)}, Counters: s.Counters,
				Interval: 5 * time.Second, ReceivedAt: time.Now(), SeqRetention: ingest.DefaultWindow.SeqRetention()}, nil
		})
		if err != nil {
			b.Fatal(err)
		}
	}
	elapsed := time.Since(start)
	if _, err := db.Writer().Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		b.Fatal(err)
	}
	fi, _ := os.Stat(filepath.Join(dir, "bench.db"))
	b.Logf("stored %d lines in %v (%.0f lines/s); database %.1f MiB = %.0f B/line incl. trigram index",
		benchLines, elapsed.Round(time.Second), benchLines/elapsed.Seconds(), float64(fi.Size())/(1<<20), float64(fi.Size())/benchLines)
	return st, hosts
}

func BenchmarkLogSearch(b *testing.B) {
	st, hosts := benchStore(b)
	all := query.LogQuery{From: time.Now().Add(-24 * time.Hour), To: time.Now().Add(time.Hour), Limit: 100}
	for _, c := range []struct {
		name  string
		q     string
		hosts bool
	}{
		{"rare_term", "RAREMARKER-500000", false},
		{"common_term", "timeout", false},
		{"two_terms_and", "refused 5432", false},
		{"or_terms", "refused OR timeout", false},
		{"phrase", `"finished job"`, false},
		{"no_text_newest_page", "", false},
		{"common_term_one_host", "timeout", true},
	} {
		b.Run(c.name, func(b *testing.B) {
			q := all
			q.Clauses, _ = query.ParseSearch(c.q)
			if c.hosts {
				q.Hosts = []domain.HostID{hosts[3].Host}
			}
			sc := hosts[0].Scope
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// A bad query plan must fail fast, not hang the run: an
				// FTS-driven join once took 50+ minutes here.
				ctx, cancel := context.WithTimeout(context.Background(), searchBudget)
				_, err := st.SearchLogs(ctx, sc, q)
				cancel()
				if err != nil {
					b.Fatalf("%s: %v (budget %v)", c.name, err, searchBudget)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N)/1000, "ms/search")
		})
	}
}
