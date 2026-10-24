package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/export"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

func exportPath(table, format string, extra ...string) string {
	now := time.Now()
	q := url.Values{"table": {table}, "format": {format},
		"from": {now.Add(-time.Hour).UTC().Format(time.RFC3339)}, "to": {now.Add(time.Hour).UTC().Format(time.RFC3339)}}
	for i := 0; i+1 < len(extra); i += 2 {
		q.Set(extra[i], extra[i+1])
	}
	return "/api/v1/export?" + q.Encode()
}

func TestExportCSVAndNDJSON(t *testing.T) {
	r := newRig(t)
	now := time.Now().Truncate(5 * time.Second)
	r.ingestSamples(t, r.a, 1, now.Add(-10*time.Second), now.Add(-5*time.Second))
	r.ingestSamples(t, r.b, 1, now.Add(-10*time.Second))
	r.ingestLogs(t, r.a, 2, logAt("exported line", nil, "app"))
	admin := r.login(t, r.a.admin)

	res := r.call(t, "GET", exportPath("metrics", "csv"), admin, "")
	recs, err := csv.NewReader(bytes.NewReader(res.body)).ReadAll()
	if res.status != 200 || err != nil || len(recs) != 1+3 || recs[0][0] != "host_id" || !strings.HasPrefix(res.header.Get("Content-Type"), "text/csv") {
		t.Fatalf("csv: %d %v %q", res.status, err, res.body) // 2 samples + the logs batch's sample, header
	}
	for _, rec := range recs[1:] {
		if rec[0] != r.a.host.String() {
			t.Fatalf("tenant A's export holds host %s", rec[0])
		}
	}
	res = r.call(t, "GET", exportPath("logs", "ndjson"), admin, "")
	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(res.body), &line); err != nil || line["message"] != "exported line" || line["host_id"] != r.a.host.String() {
		t.Fatalf("ndjson: %s (%v)", res.body, err)
	}
}

func TestExportRefusedBeforeAnyByte(t *testing.T) {
	r := newRig(t)
	admin := r.login(t, r.a.admin)
	for _, c := range []struct {
		path string
		want int
	}{
		{exportPath("users", "csv"), 400},
		{exportPath("metrics", "xml"), 400},
		{"/api/v1/export?table=metrics&from=yesterday&to=now", 400},
		{"/api/v1/export?table=metrics&from=2026-10-24T12:00:00Z&to=2026-10-24T11:00:00Z", 400},
		{exportPath("metrics", "csv", "host", r.b.host.String()), 404},
	} {
		res := r.call(t, "GET", c.path, admin, "")
		if res.status != c.want || res.header.Get("Content-Disposition") != "" || !strings.HasPrefix(res.header.Get("Content-Type"), "application/json") {
			t.Errorf("%s: %d %q %s; want a clean %d JSON error", c.path, res.status, res.header.Get("Content-Type"), res.body, c.want)
		}
	}
	if res := r.call(t, "GET", exportPath("metrics", "csv"), r.login(t, r.a.viewer), ""); res.status != 403 {
		t.Fatalf("viewer export: %d, want 403", res.status)
	}
}

// failing streams one row, then fails.
type failing struct{}

func (failing) Prepare(context.Context, domain.Scope, export.Request) ([]domain.Host, error) {
	return nil, nil
}
func (failing) Run(_ context.Context, _ domain.Scope, _ export.Request, _ []domain.Host, s export.Sink) (int, error) {
	_ = s.Columns([]string{"a"})
	_ = s.Row([]any{1})
	return 1, errors.New("disk I/O error")
}

func TestNDJSONEndsWithErrorRecordWhenStreamFails(t *testing.T) {
	a := &API{cfg: Config{Export: failing{}}}
	sess := domain.Session{Tenant: domain.NewTenantID()}
	req := httptest.NewRequest("GET", exportPath("logs", "ndjson"), nil).WithContext(context.WithValue(context.Background(), ctxKey{}, sess))
	rec := httptest.NewRecorder()
	a.handleExport(rec, req)
	var lines []string
	for s := bufio.NewScanner(rec.Body); s.Scan(); {
		lines = append(lines, s.Text())
	}
	if rec.Code != http.StatusOK || len(lines) != 2 || !strings.Contains(lines[1], `"error"`) {
		t.Fatalf("status %d lines %q: want the row then an error record", rec.Code, lines)
	}
}
