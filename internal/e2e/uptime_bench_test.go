package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/uptime"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/sqlite"
)

const benchMonitors = 1000

// monitorFleet is benchMonitors monitors of one tenant with their state,
// recorded through the real RecordCheck: result, hourly rollup, metering,
// incident and state in one transaction per check.
type monitorFleet struct {
	st     *sqlite.Store
	sc     domain.Scope
	mu     []sync.Mutex
	ms     []domain.Monitor
	states []domain.MonitorState
}

func newMonitorFleet(tb testing.TB, ctx context.Context, st *sqlite.Store, sc domain.Scope) *monitorFleet {
	f := &monitorFleet{st: st, sc: sc, mu: make([]sync.Mutex, benchMonitors)}
	for i := range benchMonitors {
		m := domain.Monitor{Name: fmt.Sprintf("site-%04d", i), Kind: domain.MonitorHTTP, Target: fmt.Sprintf("https://site-%04d.example.com/", i),
			Interval: time.Minute, Timeout: 10 * time.Second, ConfirmRetries: 1, Method: "GET", CreatedAt: time.Now()}
		id, err := st.CreateMonitor(ctx, sc, m, benchMonitors)
		if err != nil {
			tb.Fatal(err)
		}
		m.ID = id
		f.ms = append(f.ms, m)
		f.states = append(f.states, domain.MonitorState{Monitor: id, Status: domain.MonitorPending})
	}
	return f
}

// check records one check of monitor i at at; one in fifty fails, so
// incidents open and close as in a real fleet.
func (f *monitorFleet) check(ctx context.Context, i int, at time.Time, n uint64) error {
	f.mu[i].Lock()
	defer f.mu[i].Unlock()
	r := domain.CheckResult{At: at, Outcome: domain.CheckUp, Latency: 84 * time.Millisecond, TTFB: 31 * time.Millisecond, StatusCode: 200}
	if n%50 == 0 {
		r = domain.CheckResult{At: at, Outcome: domain.CheckDown, Class: domain.ErrTimeout}
	}
	step := domain.StepMonitor(f.ms[i], f.states[i], r)
	if _, err := f.st.RecordCheck(ctx, f.sc, uptime.CheckRecord{Monitor: f.ms[i], Result: r, Step: step, Revision: f.states[i].Revision}); err != nil {
		return err
	}
	f.states[i] = step.Next
	f.states[i].Revision++
	return nil
}

// BenchmarkUptimeChecks measures how many check results the store records
// per second from 8 concurrent writers, and what one costs on disk.
// monitors@60s is the fleet that rate sustains at one check a minute.
//
//	go test ./internal/e2e -run '^$' -bench UptimeChecks -benchtime 100000x
func BenchmarkUptimeChecks(b *testing.B) {
	ctx := context.Background()
	dir := b.TempDir()
	db, err := sqlite.OpenMigrated(ctx, filepath.Join(dir, "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	st, sc := benchTenant(b, ctx, db)
	f := newMonitorFleet(b, ctx, st, sc)
	before := dbBytes(b, db, dir)
	start := time.Now().Add(-48 * time.Hour)
	var next atomic.Uint64
	b.ResetTimer()
	t0 := time.Now()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				n := next.Add(1)
				if n > uint64(b.N) {
					return
				}
				i, round := int(n%benchMonitors), n/benchMonitors
				if err := f.check(ctx, i, start.Add(time.Duration(round)*time.Minute), n); err != nil {
					b.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	rate := float64(b.N) / time.Since(t0).Seconds()
	b.StopTimer()
	b.ReportMetric(rate, "checks/s")
	b.ReportMetric(rate*60, "monitors@60s")
	b.ReportMetric(float64(dbBytes(b, db, dir)-before)/float64(b.N), "disk-B/check")
}

// BenchmarkIngestCeilingWithUptime is BenchmarkIngestCeiling while 1 000
// monitors are checked once a minute, as the scheduler would, on the same
// database: the cost of uptime checking to the ingest path.
//
//	go test ./internal/e2e -run '^$' -bench IngestCeilingWithUptime -benchtime 20000x
func BenchmarkIngestCeilingWithUptime(b *testing.B) {
	var recorded atomic.Int64
	var elapsed time.Duration
	ingestCeiling(b, func(st *sqlite.Store, sc domain.Scope) func(context.Context) {
		f := newMonitorFleet(b, context.Background(), st, sc) // untimed
		return func(ctx context.Context) {
			t0 := time.Now()
			tick := time.NewTicker(time.Minute / benchMonitors) // 1 000 checks a minute, evenly spread
			defer tick.Stop()
			for n := uint64(0); ; n++ {
				select {
				case <-ctx.Done():
					elapsed = time.Since(t0)
					return
				case at := <-tick.C:
					if err := f.check(context.Background(), int(n%benchMonitors), at, n); err == nil {
						recorded.Add(1)
					}
				}
			}
		}
	})
	b.ReportMetric(float64(recorded.Load())/elapsed.Minutes(), "checks/min-during")
}

func benchTenant(b *testing.B, ctx context.Context, db *sqlite.DB) (*sqlite.Store, domain.Scope) {
	st := sqlite.NewStore(db)
	ops, _ := domain.NewOpsScope("bench")
	tid := domain.NewTenantID()
	if err := st.CreateTenant(ctx, ops, domain.Tenant{ID: tid, Name: "bench", CreatedAt: time.Now()}); err != nil {
		b.Fatal(err)
	}
	sc, _ := domain.NewScope(tid)
	return st, sc
}

// dbBytes checkpoints the WAL into the database and returns its size.
func dbBytes(b *testing.B, db *sqlite.DB, dir string) int64 {
	if err := db.Checkpoint(context.Background()); err != nil {
		b.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	return fi.Size()
}
