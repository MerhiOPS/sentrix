package export

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

var t0 = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

// fakeData is one tenant's stored data; reads honour the half-open
// [from, to) ranges and newest-first log order the real stores use.
type fakeData struct {
	hosts     []domain.Host
	core      map[domain.HostID][]domain.CoreSample
	logs      []query.LogEntry // newest first
	events    []domain.AlertEvent
	coreReads int
	logReads  int
}

func (f *fakeData) GetHost(_ context.Context, _ domain.Scope, id domain.HostID) (domain.Host, error) {
	for _, h := range f.hosts {
		if h.ID == id {
			return h, nil
		}
	}
	return domain.Host{}, domain.ErrNotFound
}
func (f *fakeData) ListHosts(context.Context, domain.Scope) ([]domain.Host, error) { return f.hosts, nil }

func (f *fakeData) RawCore(_ context.Context, _ domain.Scope, h domain.HostID, from, to time.Time) ([]domain.CoreSample, error) {
	f.coreReads++
	var out []domain.CoreSample
	for _, s := range f.core[h] {
		if !s.TS.Before(from) && s.TS.Before(to) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeData) SearchLogs(_ context.Context, _ domain.Scope, q query.LogQuery) ([]query.LogEntry, error) {
	f.logReads++
	var out []query.LogEntry
	for _, l := range f.logs {
		if q.Before != nil && !(l.Line.TS.Before(q.Before.TS) || (l.Line.TS.Equal(q.Before.TS) && l.ID < q.Before.ID)) {
			continue
		}
		if len(out) == q.Limit {
			break
		}
		out = append(out, l)
	}
	return out, nil
}

func (f *fakeData) ListEvents(context.Context, domain.Scope, int) ([]domain.AlertEvent, error) {
	return f.events, nil
}

// sink collects rows; failAfter > 0 makes Row fail once that many rows were taken.
type sink struct {
	cols      []string
	rows      [][]any
	failAfter int
}

var errSink = errors.New("client went away")

func (s *sink) Columns(c []string) error { s.cols = c; return nil }
func (s *sink) Row(v []any) error {
	if s.failAfter > 0 && len(s.rows) == s.failAfter {
		return errSink
	}
	if len(v) != len(s.cols) {
		return errors.New("row width differs from the columns")
	}
	s.rows = append(s.rows, v)
	return nil
}

func setup(t *testing.T) (*fakeData, *Exporter, domain.Scope) {
	t.Helper()
	sc, err := domain.NewScope(domain.NewTenantID())
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeData{core: map[domain.HostID][]domain.CoreSample{}}
	return f, New(f, f, f, f), sc
}

func addHost(f *fakeData, name string) domain.HostID {
	h := domain.Host{ID: domain.NewHostID(), Name: name}
	f.hosts = append(f.hosts, h)
	return h.ID
}

func TestPrepareValidatesBeforeAnythingIsWritten(t *testing.T) {
	f, e, sc := setup(t)
	addHost(f, "web-1")
	foreign := domain.NewHostID()
	cases := []struct {
		name string
		req  Request
		want error
	}{
		{"unknown table", Request{Table: "users", From: t0, To: t0.Add(time.Hour)}, domain.ErrInvalid},
		{"empty range", Request{Table: TableMetrics, From: t0, To: t0}, domain.ErrInvalid},
		{"reversed range", Request{Table: TableLogs, From: t0.Add(time.Hour), To: t0}, domain.ErrInvalid},
		{"over the maximum range", Request{Table: TableMetrics, From: t0, To: t0.Add(MaxRange + time.Second)}, domain.ErrInvalid},
		{"host of another tenant", Request{Table: TableMetrics, From: t0, To: t0.Add(time.Hour), Host: &foreign}, domain.ErrNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := e.Prepare(context.Background(), sc, c.req); !errors.Is(err, c.want) {
				t.Fatalf("err %v, want %v", err, c.want)
			}
		})
	}
	hosts, err := e.Prepare(context.Background(), sc, Request{Table: TableEvents, From: t0, To: t0.Add(MaxRange)})
	if err != nil || len(hosts) != 1 {
		t.Fatalf("exactly MaxRange over every host: %v, %d hosts", err, len(hosts))
	}
}

func TestMetricsArePagedWithoutGapsOrDuplicates(t *testing.T) {
	f, e, sc := setup(t)
	a, b := addHost(f, "a"), addHost(f, "b")
	// One sample per hour over 15 h: pages are 6 h, so samples sit exactly on
	// page boundaries (6 h, 12 h) and the last page is partial.
	for i := range 15 {
		f.core[a] = append(f.core[a], domain.CoreSample{TS: t0.Add(time.Duration(i) * time.Hour), CPUPercent: float64(i)})
	}
	f.core[b] = []domain.CoreSample{{TS: t0.Add(30 * time.Minute)}}
	req := Request{Table: TableMetrics, From: t0, To: t0.Add(15 * time.Hour)}
	hosts, err := e.Prepare(context.Background(), sc, req)
	if err != nil {
		t.Fatal(err)
	}
	s := &sink{}
	n, err := e.Run(context.Background(), sc, req, hosts, s)
	if err != nil || n != 16 || len(s.rows) != 16 {
		t.Fatalf("n=%d rows=%d err=%v, want 16 rows", n, len(s.rows), err)
	}
	seen := map[string]bool{}
	for _, r := range s.rows {
		k := r[0].(string) + r[2].(string)
		if seen[k] {
			t.Fatalf("row %v exported twice", r)
		}
		seen[k] = true
	}
	if f.coreReads != 6 {
		t.Fatalf("%d raw reads, want 3 pages × 2 hosts", f.coreReads)
	}
}

func TestLogsFollowTheCursorToTheEnd(t *testing.T) {
	f, e, sc := setup(t)
	h := addHost(f, "a")
	// Exactly two full pages, all in the same second: only the id tiebreak
	// moves the cursor forward, and the last read returns nothing.
	total := 2 * query.MaxLogLimit
	for i := range total {
		f.logs = append(f.logs, query.LogEntry{ID: int64(total - i), Host: h,
			Line: domain.LogLine{TS: t0, Source: "app", Level: domain.Level("info"), Message: "m"}})
	}
	req := Request{Table: TableLogs, From: t0, To: t0.Add(time.Hour)}
	s := &sink{}
	n, err := e.Run(context.Background(), sc, req, f.hosts, s)
	if err != nil || n != total {
		t.Fatalf("n=%d err=%v, want %d", n, err, total)
	}
	if f.logReads != 3 {
		t.Fatalf("%d reads, want 2 full pages and one empty", f.logReads)
	}
	if first, last := s.rows[0][0].(int64), s.rows[total-1][0].(int64); first != int64(total) || last != 1 {
		t.Fatalf("ids %d..%d, want %d..1", first, last, total)
	}
}

func TestAlertEventsAreFilteredByHostAndRange(t *testing.T) {
	f, e, sc := setup(t)
	a, b := addHost(f, "a"), addHost(f, "b")
	v := 91.5
	f.events = []domain.AlertEvent{
		{ID: 1, Host: a, At: t0, To: domain.StateFiring, Value: &v}, // first instant: in
		{ID: 2, Host: b, At: t0.Add(time.Minute)},                   // other host: out
		{ID: 3, Host: a, At: t0.Add(time.Hour)},                     // end is exclusive: out
		{ID: 4, Host: a, At: t0.Add(-time.Second)},                  // before: out
		{ID: 5, Host: a, At: t0.Add(30 * time.Minute)},              // in, no value
	}
	req := Request{Table: TableEvents, From: t0, To: t0.Add(time.Hour), Host: &a}
	hosts, err := e.Prepare(context.Background(), sc, req)
	if err != nil {
		t.Fatal(err)
	}
	s := &sink{}
	if _, err := e.Run(context.Background(), sc, req, hosts, s); err != nil {
		t.Fatal(err)
	}
	if len(s.rows) != 2 || s.rows[0][0] != int64(1) || s.rows[1][0] != int64(5) {
		t.Fatalf("rows %v, want events 1 and 5", s.rows)
	}
	if s.rows[0][6] != 91.5 || s.rows[1][6] != nil {
		t.Fatalf("values %v / %v, want 91.5 and an empty value", s.rows[0][6], s.rows[1][6])
	}
}

func TestASinkFailureStopsTheExportWithTheRowsWritten(t *testing.T) {
	f, e, sc := setup(t)
	h := addHost(f, "a")
	for i := range 10 {
		f.core[h] = append(f.core[h], domain.CoreSample{TS: t0.Add(time.Duration(i) * time.Minute)})
	}
	s := &sink{failAfter: 4}
	n, err := e.Run(context.Background(), sc, Request{Table: TableMetrics, From: t0, To: t0.Add(time.Hour)}, f.hosts, s)
	if !errors.Is(err, errSink) || n != 4 {
		t.Fatalf("n=%d err=%v, want 4 rows and the sink's error", n, err)
	}
}
