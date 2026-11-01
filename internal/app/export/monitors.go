package export

import (
	"context"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// MonitorReader reads a tenant's uptime data.
type MonitorReader interface {
	ListMonitors(ctx context.Context, s domain.Scope) ([]domain.Monitor, error)
	MonitorResults(ctx context.Context, s domain.Scope, monitor int64, from, to time.Time, limit int) ([]domain.CheckResult, error)
	MonitorIncidents(ctx context.Context, s domain.Scope, monitor int64, limit int) ([]domain.Incident, error)
}

// WithMonitors adds the uptime tables to the exporter.
func (e *Exporter) WithMonitors(m MonitorReader) *Exporter {
	e.monitors = m
	return e
}

// resultsWindow is how much of one monitor's checks is read per page: a
// day holds at most 2 880 checks at the 30 s floor, plus confirmations.
const (
	resultsWindow   = 24 * time.Hour
	resultsPageRows = 100_000
	maxIncidents    = 100_000
)

func (e *Exporter) monitorResults(ctx context.Context, s domain.Scope, req Request, sink Sink) (int, error) {
	if err := sink.Columns([]string{"monitor_id", "monitor", "at", "outcome", "error", "latency_ms", "ttfb_ms", "status",
		"loss", "cert_expires_at"}); err != nil {
		return 0, err
	}
	ms, err := e.monitors.ListMonitors(ctx, s)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, m := range ms {
		for from := req.From; from.Before(req.To); from = from.Add(resultsWindow) {
			rs, err := e.monitors.MonitorResults(ctx, s, m.ID, from, minTime(from.Add(resultsWindow), req.To), resultsPageRows)
			if err != nil {
				return n, err
			}
			for _, r := range rs {
				if err := sink.Row(resultRow(m, r)); err != nil {
					return n, err
				}
				n++
			}
		}
	}
	return n, nil
}

func resultRow(m domain.Monitor, r domain.CheckResult) []any {
	row := []any{m.ID, m.Name, r.At.UTC().Format(time.RFC3339Nano), string(r.Outcome), nilIfEmpty(string(r.Class)), nil, nil, nil, nil, nil}
	if r.Outcome != domain.CheckUnknown && (r.Outcome == domain.CheckUp || r.Latency > 0) {
		row[5] = r.Latency.Milliseconds()
	}
	if r.TTFB > 0 {
		row[6] = r.TTFB.Milliseconds()
	}
	if r.StatusCode > 0 {
		row[7] = r.StatusCode
	}
	if r.Loss > 0 {
		row[8] = r.Loss
	}
	if r.CertExpiry != nil {
		row[9] = r.CertExpiry.UTC().Format(time.RFC3339)
	}
	return row
}

func (e *Exporter) monitorIncidents(ctx context.Context, s domain.Scope, req Request, sink Sink) (int, error) {
	if err := sink.Columns([]string{"id", "monitor_id", "monitor", "started", "ended", "error", "failures"}); err != nil {
		return 0, err
	}
	ms, err := e.monitors.ListMonitors(ctx, s)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, m := range ms {
		inc, err := e.monitors.MonitorIncidents(ctx, s, m.ID, maxIncidents)
		if err != nil {
			return n, err
		}
		for i := len(inc) - 1; i >= 0; i-- { // oldest first, like every other table
			in := inc[i]
			if in.Start.Before(req.From) || !in.Start.Before(req.To) {
				continue
			}
			var ended any
			if in.End != nil {
				ended = in.End.UTC().Format(time.RFC3339)
			}
			if err := sink.Row([]any{in.ID, m.ID, m.Name, in.Start.UTC().Format(time.RFC3339), ended, string(in.Class), in.Failures}); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
