package uptime

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

var t0 = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// world is a fake clock, a fake store listing and a check that records
// calls and can be held open.
type world struct {
	mu      sync.Mutex
	now     time.Time
	list    []Scheduled
	calls   map[int64]int
	running map[int64]int
	maxRun  map[int64]int
	hold    chan struct{} // when set, checks block until it is closed
	result  func(Scheduled) (domain.MonitorState, error)
	s       *Scheduler
}

func newWorld(cfg SchedulerConfig) *world {
	w := &world{now: t0, calls: map[int64]int{}, running: map[int64]int{}, maxRun: map[int64]int{}}
	w.s = NewScheduler(w, w.check, cfg, w.clock, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return w
}

func (w *world) clock() time.Time { w.mu.Lock(); defer w.mu.Unlock(); return w.now }
func (w *world) advance(d time.Duration) {
	w.mu.Lock()
	w.now = w.now.Add(d)
	w.mu.Unlock()
}

func (w *world) ScheduledMonitors(context.Context, domain.OpsScope) ([]Scheduled, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]Scheduled(nil), w.list...), nil
}

func (w *world) check(_ context.Context, s Scheduled) (domain.MonitorState, error) {
	id := s.Monitor.ID
	w.mu.Lock()
	w.calls[id]++
	w.running[id]++
	w.maxRun[id] = max(w.maxRun[id], w.running[id])
	hold, result := w.hold, w.result
	w.mu.Unlock()
	if hold != nil {
		<-hold
	}
	w.mu.Lock()
	w.running[id]--
	w.mu.Unlock()
	if result != nil {
		return result(s)
	}
	st := s.State
	st.Status, st.LastCheck, st.Revision = domain.MonitorUp, w.clock(), st.Revision+1
	return st, nil
}

func (w *world) tick(t *testing.T) {
	t.Helper()
	if err := w.s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.s.Wait()
}

func sched(id int64, tenant domain.TenantID, last time.Time) Scheduled {
	return Scheduled{Tenant: tenant, Monitor: domain.Monitor{ID: id, Interval: time.Minute},
		State: domain.MonitorState{Monitor: id, Status: domain.MonitorUp, LastCheck: last}}
}

func TestSlotsSpreadAcrossTheInterval(t *testing.T) {
	seconds := map[int64]int{}
	for id := int64(1); id <= 100; id++ {
		next := nextSlot(domain.Monitor{ID: id, Interval: time.Minute}, t0)
		if !next.After(t0) || next.Sub(t0) > time.Minute {
			t.Fatalf("monitor %d next slot %v: want within (t0, t0+1m]", id, next)
		}
		if again := nextSlot(domain.Monitor{ID: id, Interval: time.Minute}, next); again.Sub(next) != time.Minute {
			t.Fatalf("monitor %d: slots %v then %v, want one interval apart", id, next, again)
		}
		seconds[next.Unix()]++
	}
	crowded := 0
	for _, n := range seconds {
		crowded = max(crowded, n)
	}
	// Without phases all 100 fire in the same second. Random placement of
	// 100 into 60 seconds gives ~49 distinct seconds and a busiest second
	// of 4-7; these bounds hold for it with overwhelming probability.
	if len(seconds) < 35 || crowded > 10 {
		t.Fatalf("100 monitors over a minute: %d distinct seconds, %d in the busiest: not spread", len(seconds), crowded)
	}
}

func TestRestartDoesNotCheckEverythingAtOnce(t *testing.T) {
	w := newWorld(DefaultSchedulerConfig)
	tenant := domain.NewTenantID()
	for id := int64(1); id <= 60; id++ {
		w.list = append(w.list, sched(id, tenant, t0.Add(-3*time.Hour))) // the server was down for hours
	}
	w.list = append(w.list, sched(100, tenant, time.Time{})) // never checked: first check now
	w.tick(t)
	if len(w.calls) != 1 || w.calls[100] != 1 {
		t.Fatalf("first tick after restart checked %v: only the never-checked monitor may run at once", w.calls)
	}
	for range 60 {
		w.advance(time.Second)
		w.tick(t)
	}
	if len(w.calls) != 61 {
		t.Fatalf("after one interval %d monitors were checked, want all 61", len(w.calls))
	}
	for id, n := range w.calls {
		if n > 2 {
			t.Fatalf("monitor %d checked %d times in one interval", id, n)
		}
	}
}

func TestOneCheckPerMonitorAtATime(t *testing.T) {
	w := newWorld(DefaultSchedulerConfig)
	w.list = []Scheduled{sched(1, domain.NewTenantID(), time.Time{})}
	w.hold = make(chan struct{})
	if err := w.s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		w.advance(2 * time.Minute) // overdue again while the first check hangs
		w.s.CheckNow(1)
		if err := w.s.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	close(w.hold)
	w.s.Wait()
	if w.maxRun[1] != 1 || w.calls[1] != 1 {
		t.Fatalf("calls %d, concurrent %d: a monitor must never be checked twice at once", w.calls[1], w.maxRun[1])
	}
}

func TestFailedCheckIsRetriedSoon(t *testing.T) {
	w := newWorld(DefaultSchedulerConfig)
	w.list = []Scheduled{sched(1, domain.NewTenantID(), time.Time{})}
	w.result = func(s Scheduled) (domain.MonitorState, error) {
		st := s.State
		st.Failures, st.Revision = 1, st.Revision+1 // failed, not confirmed
		return st, nil
	}
	w.tick(t)
	w.advance(9 * time.Second)
	w.tick(t)
	if w.calls[1] != 1 {
		t.Fatal("the confirmation re-check ran before its delay")
	}
	w.advance(time.Second)
	w.tick(t)
	if w.calls[1] != 2 {
		t.Fatalf("calls %d: the confirmation re-check is due 10 s after a failure", w.calls[1])
	}
}

func TestWorkersAreSharedAcrossTenants(t *testing.T) {
	w := newWorld(SchedulerConfig{Workers: 2, PerTenant: 1, Reload: time.Hour})
	busy, quiet := domain.NewTenantID(), domain.NewTenantID()
	for id := int64(1); id <= 5; id++ {
		w.list = append(w.list, sched(id, busy, time.Time{}))
	}
	w.list = append(w.list, sched(9, quiet, time.Time{}))
	w.hold = make(chan struct{})
	if err := w.s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := w.s.Stats(); st.Running != 2 {
		t.Fatalf("%d checks running, want the 2 workers", st.Running)
	}
	close(w.hold)
	w.s.Wait()
	if w.calls[9] != 1 {
		t.Fatalf("calls %v: the quiet tenant's monitor must get a worker beside the busy tenant", w.calls)
	}
}

func TestSaturationSkipsAfterAWholeInterval(t *testing.T) {
	w := newWorld(SchedulerConfig{Workers: 1, Reload: time.Hour})
	tenant := domain.NewTenantID()
	w.list = []Scheduled{sched(1, tenant, time.Time{}), sched(2, tenant, time.Time{})}
	w.hold = make(chan struct{})
	_ = w.s.Tick(context.Background()) // one starts and hangs; the other waits
	w.advance(61 * time.Second)
	_ = w.s.Tick(context.Background())
	close(w.hold)
	w.s.Wait()
	if st := w.s.Stats(); st.Skipped != 1 || st.MaxLateness != 0 {
		t.Fatalf("stats %+v: a check waiting longer than its interval is skipped and counted", st)
	}
}

func TestReloadDropsPausedMonitorsAndPicksUpNewOnes(t *testing.T) {
	w := newWorld(SchedulerConfig{Workers: 4, Reload: time.Hour})
	tenant := domain.NewTenantID()
	w.list = []Scheduled{sched(1, tenant, time.Time{})}
	w.tick(t)
	w.mu.Lock()
	w.list = []Scheduled{sched(2, tenant, time.Time{})} // 1 paused, 2 created
	w.mu.Unlock()
	w.s.Refresh()
	w.advance(5 * time.Minute)
	w.tick(t)
	if w.calls[1] != 1 || w.calls[2] != 1 || w.s.Stats().Monitors != 1 {
		t.Fatalf("calls %v, scheduled %d: the paused monitor must stop, the new one start at once", w.calls, w.s.Stats().Monitors)
	}
	if w.s.CheckNow(1) {
		t.Fatal("CheckNow accepted a monitor that is no longer scheduled")
	}
}

func TestStaleResultTriggersReload(t *testing.T) {
	w := newWorld(SchedulerConfig{Workers: 4, Reload: time.Hour})
	w.list = []Scheduled{sched(1, domain.NewTenantID(), time.Time{})}
	w.result = func(s Scheduled) (domain.MonitorState, error) { return s.State, ErrStale }
	w.tick(t)
	w.s.mu.Lock()
	dirty := w.s.dirty
	w.s.mu.Unlock()
	if !dirty || w.s.Stats().Stale != 1 {
		t.Fatalf("after a stale result: dirty %v, stale %d", dirty, w.s.Stats().Stale)
	}
}
