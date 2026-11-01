package uptime

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// SchedulerConfig bounds the scheduler.
type SchedulerConfig struct {
	Workers   int           // checks running at once, across tenants
	PerTenant int           // of those, at most this many for one tenant
	Reload    time.Duration // how often monitor definitions are re-read
}

// DefaultSchedulerConfig suits a small server.
var DefaultSchedulerConfig = SchedulerConfig{Workers: 64, PerTenant: 16, Reload: 15 * time.Second}

// CheckFunc runs one check (Checker.Check in production).
type CheckFunc func(ctx context.Context, s Scheduled) (domain.MonitorState, error)

// Scheduler decides when each monitor is checked. It is driven by Tick, so
// tests run it on a fake clock.
type Scheduler struct {
	reader ScheduleReader
	check  CheckFunc
	cfg    SchedulerConfig
	now    func() time.Time
	log    *slog.Logger

	mu        sync.Mutex
	entries   map[int64]*entry
	loaded    time.Time
	dirty     bool
	running   int
	perTenant map[domain.TenantID]int
	stats     SchedulerStats
	wg        sync.WaitGroup
}

type entry struct {
	s       Scheduled
	due     time.Time
	running bool
	removed bool // gone from the store while its check ran
}

// SchedulerStats is what /api/v1/system shows about uptime checking.
type SchedulerStats struct {
	Monitors int
	Running  int
	Checks   uint64
	// Skipped counts checks dropped because no worker was free for a
	// whole interval; MaxLateness is the worst start delay since start.
	Skipped     uint64
	Stale       uint64
	MaxLateness time.Duration
}

// NewScheduler builds a scheduler.
func NewScheduler(reader ScheduleReader, check CheckFunc, cfg SchedulerConfig, now func() time.Time, log *slog.Logger) *Scheduler {
	if cfg.PerTenant <= 0 || cfg.PerTenant > cfg.Workers {
		cfg.PerTenant = cfg.Workers
	}
	return &Scheduler{reader: reader, check: check, cfg: cfg, now: now, log: log,
		entries: map[int64]*entry{}, perTenant: map[domain.TenantID]int{}}
}

// Refresh makes the next Tick re-read the monitors (after an edit).
func (s *Scheduler) Refresh() {
	s.mu.Lock()
	s.dirty = true
	s.mu.Unlock()
}

// CheckNow makes a monitor due at once. It reports false if the monitor is
// not scheduled (paused, unknown) or its check is already running.
func (s *Scheduler) CheckNow(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok || e.running {
		return false
	}
	e.due = s.now()
	return true
}

// Stats reports the scheduler's counters.
func (s *Scheduler) Stats() SchedulerStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.stats
	st.Monitors, st.Running = len(s.entries), s.running
	return st
}

// Run ticks every second until ctx ends, then waits for running checks.
func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.wg.Wait()
			return
		case <-t.C:
			if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
				s.log.Error("uptime scheduling", "err", err)
			}
		}
	}
}

// Wait blocks until every started check has finished (tests).
func (s *Scheduler) Wait() { s.wg.Wait() }

// Tick reloads monitors when due and starts every check that is due and has
// a free worker.
func (s *Scheduler) Tick(ctx context.Context) error {
	now := s.now()
	s.mu.Lock()
	reload := s.dirty || s.loaded.IsZero() || now.Sub(s.loaded) >= s.cfg.Reload
	s.mu.Unlock()
	if reload {
		ops, err := domain.NewOpsScope("uptime scheduling")
		if err != nil {
			return err
		}
		list, err := s.reader.ScheduledMonitors(ctx, ops)
		if err != nil {
			return err
		}
		s.apply(list, now)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.dueLocked(now) {
		t := e.s.Tenant
		if s.running >= s.cfg.Workers || s.perTenant[t] >= s.cfg.PerTenant {
			if now.Sub(e.due) > e.s.Monitor.Interval {
				s.stats.Skipped++
				e.due = nextSlot(e.s.Monitor, now)
			}
			continue
		}
		s.stats.MaxLateness = max(s.stats.MaxLateness, now.Sub(e.due))
		e.running = true
		s.running++
		s.perTenant[t]++
		s.wg.Add(1)
		go s.run(ctx, e, e.s)
	}
	return nil
}

// dueLocked returns the due, idle entries, most overdue first.
func (s *Scheduler) dueLocked(now time.Time) []*entry {
	var due []*entry
	for _, e := range s.entries {
		if !e.running && !e.due.After(now) {
			due = append(due, e)
		}
	}
	sort.Slice(due, func(i, j int) bool {
		if !due[i].due.Equal(due[j].due) {
			return due[i].due.Before(due[j].due)
		}
		return due[i].s.Monitor.ID < due[j].s.Monitor.ID
	})
	return due
}

func (s *Scheduler) run(ctx context.Context, e *entry, sch Scheduled) {
	defer s.wg.Done()
	started := s.now()
	next, err := s.check(ctx, sch)
	s.mu.Lock()
	defer s.mu.Unlock()
	e.running = false
	s.running--
	s.perTenant[sch.Tenant]--
	s.stats.Checks++
	switch {
	case errors.Is(err, ErrStale):
		s.stats.Stale++
		s.dirty = true // re-read the monitor; the reload sets its next check
	case err != nil:
		if ctx.Err() == nil {
			s.log.Error("uptime check", "monitor", sch.Monitor.ID, "err", err)
		}
		e.due = nextSlot(sch.Monitor, started)
	case next.NeedsRetry():
		e.s.State = next
		e.due = started.Add(sch.Monitor.RetryDelay())
	default:
		e.s.State = next
		e.due = nextSlot(sch.Monitor, started)
	}
	if e.removed {
		delete(s.entries, sch.Monitor.ID)
	}
}

// apply merges a fresh listing: new monitors are scheduled, changed ones
// rescheduled, missing ones (paused, deleted) dropped.
func (s *Scheduler) apply(list []Scheduled, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[int64]bool{}
	for _, sch := range list {
		id := sch.Monitor.ID
		seen[id] = true
		e, ok := s.entries[id]
		switch {
		case !ok:
			s.entries[id] = &entry{s: sch, due: firstDue(sch, now)}
		case e.running:
			e.removed = false // its own completion sets the next check
		case sch.State.Revision > e.s.State.Revision || e.s.Monitor.Interval != sch.Monitor.Interval:
			// Changed in the store (pause, resume, edit): start over from it.
			e.s, e.due = sch, firstDue(sch, now)
		default:
			// Revisions only grow: a listing read before our last check
			// committed is older than what we hold, so keep our state.
			e.s.Monitor = sch.Monitor
		}
	}
	for id, e := range s.entries {
		if !seen[id] {
			if e.running {
				e.removed = true
			} else {
				delete(s.entries, id)
			}
		}
	}
	s.loaded, s.dirty = now, false
}

// firstDue is when a monitor just (re)read is next checked. A monitor never
// checked, or mid-confirmation, goes now; otherwise the schedule resumes
// from its last check, and one that is overdue (the server was down) takes
// its next slot rather than now, so a restart does not check everything at
// once.
func firstDue(sch Scheduled, now time.Time) time.Time {
	st, m := sch.State, sch.Monitor
	if st.LastCheck.IsZero() || st.NeedsRetry() {
		return now
	}
	if next := nextSlot(m, st.LastCheck); next.After(now) {
		return next
	}
	return nextSlot(m, now)
}

// nextSlot is the first time after t on the monitor's grid: multiples of its
// interval shifted by a stable per-monitor phase, so monitors sharing an
// interval spread across it instead of all firing on the same second.
func nextSlot(m domain.Monitor, t time.Time) time.Time {
	iv := m.Interval.Nanoseconds()
	if iv <= 0 {
		return t.Add(time.Minute)
	}
	off := phase(m.ID, iv)
	n := t.UnixNano() - off
	k := n / iv
	if n < 0 && n%iv != 0 {
		k--
	}
	return time.Unix(0, (k+1)*iv+off)
}

// phase is a stable offset in [0, interval) for a monitor id: splitmix64,
// so consecutive ids land far apart.
func phase(id, interval int64) int64 {
	z := uint64(id) + 0x9e3779b97f4a7c15 //nolint:gosec // ids are positive; any bit pattern mixes
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	z ^= z >> 31
	return int64(z % uint64(interval)) //nolint:gosec // the remainder is below interval, an int64
}
