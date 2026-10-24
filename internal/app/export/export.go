// Package export streams a tenant's own data out (data portability):
// metrics, logs and alert events, in bounded pages through the same scoped
// readers the dashboard uses, so an export can never see another tenant.
package export

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Table is what is exported.
type Table string

const (
	TableMetrics Table = "metrics"
	TableLogs    Table = "logs"
	TableEvents  Table = "alert_events"
)

// MaxRange bounds one export; longer histories are exported in parts.
const MaxRange = 400 * 24 * time.Hour

// metricsWindow is how much raw data is read per page and host.
const metricsWindow = 6 * time.Hour

// Request is a validated export.
type Request struct {
	Table    Table
	From, To time.Time
	Host     *domain.HostID // nil: every host
}

// Sink receives the export: columns once, then rows of the same length.
type Sink interface {
	Columns(cols []string) error
	Row(vals []any) error
}

// EventLister reads alert events.
type EventLister interface {
	ListEvents(ctx context.Context, s domain.Scope, limit int) ([]domain.AlertEvent, error)
}

// Exporter runs exports.
type Exporter struct {
	hosts  query.HostReader
	core   query.CoreReader
	logs   query.LogReader
	events EventLister
}

// New builds an exporter.
func New(h query.HostReader, c query.CoreReader, l query.LogReader, e EventLister) *Exporter {
	return &Exporter{hosts: h, core: c, logs: l, events: e}
}

// Prepare validates a request completely — table, range and host — so the
// caller can refuse it before writing any response header (OLD B12).
func (e *Exporter) Prepare(ctx context.Context, s domain.Scope, req Request) ([]domain.Host, error) {
	switch req.Table {
	case TableMetrics, TableLogs, TableEvents:
	default:
		return nil, fmt.Errorf("%w: table must be metrics, logs or alert_events", domain.ErrInvalid)
	}
	if !req.From.Before(req.To) || req.To.Sub(req.From) > MaxRange {
		return nil, fmt.Errorf("%w: from must be before to, at most 400 days apart", domain.ErrInvalid)
	}
	if req.Host != nil {
		h, err := e.hosts.GetHost(ctx, s, *req.Host)
		if err != nil {
			return nil, err
		}
		return []domain.Host{h}, nil
	}
	return e.hosts.ListHosts(ctx, s)
}

// Run streams the export into sink and returns the rows written. An error
// after the first row means the output is incomplete; the sink's format
// decides how to say so.
func (e *Exporter) Run(ctx context.Context, s domain.Scope, req Request, hosts []domain.Host, sink Sink) (int, error) {
	switch req.Table {
	case TableMetrics:
		return e.metrics(ctx, s, req, hosts, sink)
	case TableLogs:
		return e.logLines(ctx, s, req, hosts, sink)
	default:
		return e.alertEvents(ctx, s, req, hosts, sink)
	}
}

func (e *Exporter) metrics(ctx context.Context, s domain.Scope, req Request, hosts []domain.Host, sink Sink) (int, error) {
	err := sink.Columns([]string{"host_id", "host", "ts", "cpu_percent", "mem_percent", "disk_percent", "load1", "load5",
		"load15", "mem_used_bytes", "net_rx_bytes", "net_tx_bytes", "disk_read_bytes", "disk_write_bytes"})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, h := range hosts {
		for from := req.From; from.Before(req.To); from = from.Add(metricsWindow) {
			samples, err := e.core.RawCore(ctx, s, h.ID, from, minTime(from.Add(metricsWindow), req.To))
			if err != nil {
				return n, err
			}
			for _, x := range samples {
				if err := sink.Row([]any{h.ID.String(), h.Name, x.TS.UTC().Format(time.RFC3339), x.CPUPercent, x.MemoryPercent,
					x.DiskPercent, x.Load1, x.Load5, x.Load15, x.MemoryUsed, x.NetRxBytes, x.NetTxBytes, x.DiskReadBytes,
					x.DiskWriteBytes}); err != nil {
					return n, err
				}
				n++
			}
		}
	}
	return n, nil
}

func (e *Exporter) logLines(ctx context.Context, s domain.Scope, req Request, hosts []domain.Host, sink Sink) (int, error) {
	if err := sink.Columns([]string{"id", "host_id", "ts", "source", "level", "message", "labels"}); err != nil {
		return 0, err
	}
	q := query.LogQuery{From: req.From, To: req.To, Limit: query.MaxLogLimit}
	if req.Host != nil {
		q.Hosts = []domain.HostID{*req.Host}
	}
	n := 0
	for {
		page, err := e.logs.SearchLogs(ctx, s, q)
		if err != nil {
			return n, err
		}
		for _, l := range page {
			labels, _ := json.Marshal(l.Line.Labels)
			if err := sink.Row([]any{l.ID, l.Host.String(), l.Line.TS.UTC().Format(time.RFC3339Nano), l.Line.Source,
				string(l.Line.Level), l.Line.Message, string(labels)}); err != nil {
				return n, err
			}
			n++
		}
		if len(page) < q.Limit {
			return n, nil
		}
		last := page[len(page)-1]
		q.Before = &query.LogCursor{TS: last.Line.TS, ID: last.ID}
	}
}

func (e *Exporter) alertEvents(ctx context.Context, s domain.Scope, req Request, hosts []domain.Host, sink Sink) (int, error) {
	if err := sink.Columns([]string{"id", "rule_id", "host_id", "from", "to", "at", "value", "notified", "outcome", "delivery"}); err != nil {
		return 0, err
	}
	keep := map[domain.HostID]bool{}
	for _, h := range hosts {
		keep[h.ID] = true
	}
	events, err := e.events.ListEvents(ctx, s, maxEvents)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, ev := range events {
		if !keep[ev.Host] || ev.At.Before(req.From) || !ev.At.Before(req.To) {
			continue
		}
		var v any
		if ev.Value != nil {
			v = *ev.Value
		}
		if err := sink.Row([]any{ev.ID, ev.Rule, ev.Host.String(), string(ev.From), string(ev.To), ev.At.UTC().Format(time.RFC3339),
			v, ev.Notify, ev.Outcome, ev.Delivery}); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// maxEvents bounds the alert history exported (it is short-lived history).
const maxEvents = 100_000

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
