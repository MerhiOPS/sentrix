package export

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

type fakeMonitors struct {
	monitors  []domain.Monitor
	results   map[int64][]domain.CheckResult
	incidents map[int64][]domain.Incident // newest first, as the store returns them
	pages     int
}

func (f *fakeMonitors) ListMonitors(context.Context, domain.Scope) ([]domain.Monitor, error) {
	return f.monitors, nil
}
func (f *fakeMonitors) MonitorResults(_ context.Context, _ domain.Scope, id int64, from, to time.Time, _ int) ([]domain.CheckResult, error) {
	f.pages++
	var out []domain.CheckResult
	for _, r := range f.results[id] {
		if !r.At.Before(from) && r.At.Before(to) {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeMonitors) MonitorIncidents(_ context.Context, _ domain.Scope, id int64, _ int) ([]domain.Incident, error) {
	return f.incidents[id], nil
}

func TestUptimeTablesExport(t *testing.T) {
	_, e, sc := setup(t)
	exp := t0.Add(30 * 24 * time.Hour)
	end := t0.Add(2 * time.Hour)
	m := &fakeMonitors{
		monitors: []domain.Monitor{{ID: 1, Name: "shop"}, {ID: 2, Name: "db"}},
		results: map[int64][]domain.CheckResult{
			1: {{At: t0, Outcome: domain.CheckUp, Latency: 80 * time.Millisecond, StatusCode: 200, CertExpiry: &exp},
				{At: t0.Add(30 * time.Hour), Outcome: domain.CheckDown, Class: domain.ErrTimeout}},
			2: {{At: t0.Add(time.Hour), Outcome: domain.CheckUnknown, Class: domain.ErrConnect}},
		},
		incidents: map[int64][]domain.Incident{1: {
			{ID: 9, Start: t0.Add(72 * time.Hour), Class: domain.ErrDNS, Failures: 2},           // after the range
			{ID: 8, Start: t0.Add(time.Hour), End: &end, Class: domain.ErrTimeout, Failures: 3}, // in
			{ID: 7, Start: t0.Add(-time.Hour), End: &end, Class: domain.ErrStatus, Failures: 1}, // before
		}},
	}
	e.WithMonitors(m)
	req := Request{Table: TableMonitorResults, From: t0, To: t0.Add(48 * time.Hour)}
	if _, err := e.Prepare(context.Background(), sc, req); err != nil {
		t.Fatal(err)
	}
	s := &sink{}
	if n, err := e.Run(context.Background(), sc, req, nil, s); err != nil || n != 3 {
		t.Fatalf("results: %d rows, %v; want 3 across two days and two monitors", n, err)
	}
	up, down, unknown := s.rows[0], s.rows[1], s.rows[2]
	if up[3] != "up" || up[4] != nil || up[5] != int64(80) || up[7] != 200 || up[9] == nil {
		t.Fatalf("up row %v", up)
	}
	if down[4] != "timeout" || down[5] != nil || down[7] != nil {
		t.Fatalf("down row %v: no latency or status for a timeout", down)
	}
	if unknown[0] != int64(2) || unknown[3] != "unknown" || unknown[5] != nil {
		t.Fatalf("unknown row %v", unknown)
	}
	if m.pages != 4 {
		t.Fatalf("%d result reads, want one per day per monitor", m.pages)
	}

	req.Table = TableMonitorIncidents
	s = &sink{}
	if n, err := e.Run(context.Background(), sc, req, nil, s); err != nil || n != 1 || s.rows[0][0] != int64(8) || s.rows[0][5] != "timeout" {
		t.Fatalf("incidents: %d %v %v; want only the one started in range", n, err, s.rows)
	}
}

func TestUptimeTablesRefusedWithoutMonitorsOrWithAHost(t *testing.T) {
	f, e, sc := setup(t)
	h := addHost(f, "a")
	req := Request{Table: TableMonitorResults, From: t0, To: t0.Add(time.Hour)}
	if _, err := e.Prepare(context.Background(), sc, req); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("no monitor reader: %v, want ErrInvalid", err)
	}
	e.WithMonitors(&fakeMonitors{})
	req.Host = &h
	if _, err := e.Prepare(context.Background(), sc, req); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("a host on an uptime table: %v, want ErrInvalid", err)
	}
}
