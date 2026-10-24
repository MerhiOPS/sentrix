package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/export"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Exporter streams a tenant's data out.
type Exporter interface {
	Prepare(ctx context.Context, s domain.Scope, req export.Request) ([]domain.Host, error)
	Run(ctx context.Context, s domain.Scope, req export.Request, hosts []domain.Host, sink export.Sink) (int, error)
}

// handleExport: GET /api/v1/export?table&from&to&format=csv|ndjson[&host].
// Everything is validated before the first byte, so a bad request is a
// clean JSON error, never a half-written file (OLD B12). A failure after
// that cannot change the status: NDJSON ends with an error record.
func (a *API) handleExport(w http.ResponseWriter, r *http.Request) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	req, format, err := parseExport(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	hosts, err := a.cfg.Export.Prepare(r.Context(), scope, req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	name := fmt.Sprintf("sentrix-%s-%s.%s", req.Table, req.From.UTC().Format("20060102"), format)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	var sink interface {
		export.Sink
		fail(error)
		flush()
	}
	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		sink = &csvSink{w: csv.NewWriter(w), flusher: http.NewResponseController(w)}
	} else {
		w.Header().Set("Content-Type", "application/x-ndjson")
		sink = &ndjsonSink{enc: json.NewEncoder(w), flusher: http.NewResponseController(w)}
	}
	w.WriteHeader(http.StatusOK)
	n, err := a.cfg.Export.Run(r.Context(), scope, req, hosts, sink)
	if err != nil {
		slog.Error("export failed mid-stream", "table", req.Table, "rows", n, "err", err)
		sink.fail(err)
	}
	sink.flush()
}

func parseExport(r *http.Request) (export.Request, string, error) {
	q := r.URL.Query()
	format := q.Get("format")
	if format == "" {
		format = "ndjson"
	}
	if format != "csv" && format != "ndjson" {
		return export.Request{}, "", fmt.Errorf("%w: format must be csv or ndjson", domain.ErrInvalid)
	}
	from, err1 := time.Parse(time.RFC3339, q.Get("from"))
	to, err2 := time.Parse(time.RFC3339, q.Get("to"))
	if err1 != nil || err2 != nil {
		return export.Request{}, "", fmt.Errorf("%w: from and to must be RFC 3339 times", domain.ErrInvalid)
	}
	req := export.Request{Table: export.Table(q.Get("table")), From: from, To: to}
	if h := q.Get("host"); h != "" {
		id, err := domain.ParseHostID(h)
		if err != nil {
			return export.Request{}, "", fmt.Errorf("host: %w", domain.ErrNotFound)
		}
		req.Host = &id
	}
	return req, format, nil
}

// flushEvery bounds how much is buffered before it reaches the client.
const flushEvery = 1000

type csvSink struct {
	w       *csv.Writer
	flusher *http.ResponseController
	rows    int
}

func (s *csvSink) Columns(cols []string) error { return s.w.Write(cols) }

func (s *csvSink) Row(vals []any) error {
	rec := make([]string, len(vals))
	for i, v := range vals {
		rec[i] = cell(v)
	}
	if err := s.w.Write(rec); err != nil {
		return err
	}
	if s.rows++; s.rows%flushEvery == 0 {
		s.flush()
	}
	return s.w.Error()
}

// fail: CSV has no way to carry an error after the header; the file simply
// ends early. Documented in the API; NDJSON is the format to use when
// completeness must be checked.
func (s *csvSink) fail(error) {}

func (s *csvSink) flush() {
	s.w.Flush()
	_ = s.flusher.Flush()
}

type ndjsonSink struct {
	enc     *json.Encoder
	flusher *http.ResponseController
	cols    []string
	rows    int
}

func (s *ndjsonSink) Columns(cols []string) error { s.cols = cols; return nil }

func (s *ndjsonSink) Row(vals []any) error {
	m := make(map[string]any, len(vals))
	for i, v := range vals {
		m[s.cols[i]] = v
	}
	if err := s.enc.Encode(m); err != nil {
		return err
	}
	if s.rows++; s.rows%flushEvery == 0 {
		s.flush()
	}
	return nil
}

func (s *ndjsonSink) fail(error) {
	_ = s.enc.Encode(map[string]string{"error": "export failed before completion; the rows above are incomplete"})
}

func (s *ndjsonSink) flush() { _ = s.flusher.Flush() }

func cell(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return fmt.Sprint(x)
	}
}
