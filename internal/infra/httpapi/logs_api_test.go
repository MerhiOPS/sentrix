package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

func (r *rig) ingestLogs(t *testing.T, tr tenantRig, seq uint64, lines ...domain.LogLine) ingest.Result {
	t.Helper()
	ah := ingest.AuthHost{Tenant: tr.tenant, Host: tr.host, Interval: 5 * time.Second, Enabled: true}
	res, err := r.ingest.Ingest(context.Background(), ah, ingest.Batch{Seq: seq, Logs: lines,
		Samples: []domain.CoreSample{{TS: time.Now(), CPUPercent: 5}}})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func logAt(msg string, labels domain.Labels, source string) domain.LogLine {
	return domain.LogLine{TS: time.Now(), Source: source, Level: domain.LevelError, Message: msg, Labels: labels}
}

func (r *rig) searchLogs(t *testing.T, cookie, q string) (int, logPageDTO) {
	t.Helper()
	now := time.Now()
	path := "/api/v1/logs?from=" + url.QueryEscape(now.Add(-time.Hour).Format(time.RFC3339)) +
		"&to=" + url.QueryEscape(now.Add(time.Hour).Format(time.RFC3339)) + "&q=" + url.QueryEscape(q)
	res := r.call(t, "GET", path, cookie, "")
	var page logPageDTO
	_ = json.Unmarshal(res.body, &page)
	return res.status, page
}

// TestSecretsRedactedOnTheStoredRow: Phase 4 acceptance — asserted on what
// search returns from storage, not on the redactor's output.
func TestSecretsRedactedOnTheStoredRow(t *testing.T) {
	r := newRig(t)
	r.ingestLogs(t, r.a, 1, logAt("login failed", domain.Labels{"user": "jane@example.com", "auth": "Bearer abcdefghijklmnop"},
		"/var/log/app?password=hunter22"))
	status, page := r.searchLogs(t, r.login(t, r.a.viewer), "login")
	if status != 200 || len(page.Entries) != 1 {
		t.Fatalf("search: %d %+v", status, page)
	}
	e := page.Entries[0]
	raw, _ := json.Marshal(e)
	for _, secret := range []string{"jane@example.com", "abcdefghijklmnop", "hunter22"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("stored row still contains %q: %s", secret, raw)
		}
	}
}

func TestFTSSyntaxIsSearchedLiterally(t *testing.T) {
	r := newRig(t)
	r.ingestLogs(t, r.a, 1, logAt(`parse error near title:xyz AND NEAR(abc)`, nil, "app"))
	c := r.login(t, r.a.viewer)
	for _, q := range []string{"title:xyz", "NEAR(abc)", `"AND NEAR"`} {
		if status, page := r.searchLogs(t, c, q); status != 200 || len(page.Entries) != 1 {
			t.Errorf("q=%q: %d, %d entries", q, status, len(page.Entries))
		}
	}
	if status, _ := r.searchLogs(t, c, "ab"); status != http.StatusBadRequest {
		t.Errorf("two-character term: %d, want 400 (never a silent full scan)", status)
	}
}

func TestOverQuotaTenantStillIngestsMetrics(t *testing.T) {
	r := newRig(t)
	ops, _ := domain.NewOpsScope("test")
	if err := r.store.SetLogLimit(context.Background(), ops, r.a.tenant, 10); err != nil {
		t.Fatal(err)
	}
	res := r.ingestLogs(t, r.a, 1, logAt("this line is longer than ten bytes", nil, "app"))
	if res.Stored != 1 || res.LogsShed != 1 || res.LogsShedReason != ingest.ShedQuota {
		t.Fatalf("over quota: %+v — metrics must be stored and logs shed", res)
	}
}

func TestTenantCannotSearchAnotherTenantsLogs(t *testing.T) {
	r := newRig(t)
	r.ingestLogs(t, r.a, 1, logAt("tenant-a-only-line", nil, "app"))
	if _, page := r.searchLogs(t, r.login(t, r.b.viewer), "tenant-a-only"); len(page.Entries) != 0 {
		t.Fatalf("tenant B found tenant A's line: %+v", page.Entries)
	}
}

func TestMalformedCursorIs400(t *testing.T) {
	r := newRig(t)
	now := time.Now()
	path := "/api/v1/logs?from=" + url.QueryEscape(now.Add(-time.Hour).Format(time.RFC3339)) +
		"&to=" + url.QueryEscape(now.Format(time.RFC3339)) + "&cursor=%21%21"
	if res := r.call(t, "GET", path, r.login(t, r.a.viewer), ""); res.status != http.StatusBadRequest {
		t.Fatalf("malformed cursor: %d, want 400 (OLD B9)", res.status)
	}
}

func TestNewRuleAppliesToTheNextBatch(t *testing.T) {
	r := newRig(t)
	if res := r.call(t, "POST", "/api/v1/redaction-rules", r.login(t, r.a.viewer), `{"pattern":"acct-\\d+","action":"mask"}`); res.status != http.StatusForbidden {
		t.Fatalf("viewer created a rule: %d", res.status)
	}
	admin := r.login(t, r.a.admin)
	// Warm the redactor's per-tenant cache first, so only invalidation — not
	// an empty cache — can make the new rule apply to the next batch.
	r.ingestLogs(t, r.a, 1, logAt("warm the cache", nil, "app"))
	if res := r.call(t, "POST", "/api/v1/redaction-rules", admin, `{"pattern":"acct-\\d+","action":"mask"}`); res.status != http.StatusCreated {
		t.Fatalf("admin rule: %d %s", res.status, res.body)
	}
	if res := r.call(t, "POST", "/api/v1/redaction-rules", admin, `{"pattern":"(?<=x)y","action":"mask"}`); res.status != http.StatusBadRequest {
		t.Fatalf("non-RE2 pattern: %d, want 400", res.status)
	}
	r.ingestLogs(t, r.a, 2, logAt("charged acct-123456", nil, "app"))
	_, page := r.searchLogs(t, admin, "charged")
	if len(page.Entries) != 1 || strings.Contains(page.Entries[0].Message, "123456") {
		t.Fatalf("rule not applied on the next batch (cache invalidation): %+v", page.Entries)
	}
}

func TestLiveTailFiltersAndIsolation(t *testing.T) {
	r := newRig(t)
	ca, cb := r.login(t, r.a.viewer), r.login(t, r.b.viewer)
	tailA, _, err := r.dial(t, ca, testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	defer tailA.CloseNow() //nolint:errcheck
	tailB, _, err := r.dial(t, cb, testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	defer tailB.CloseNow() //nolint:errcheck
	for _, c := range []*websocket.Conn{tailA, tailB} {
		if err := c.Write(context.Background(), websocket.MessageText, []byte(`{"type":"tail","hosts":null,"q":"timeout"}`)); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(50 * time.Millisecond)
	r.ingestLogs(t, r.a, 1, logAt("disk ok", nil, "app"), logAt("upstream timeout", domain.Labels{"ip": "10.0.0.9"}, "app"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, data, err := tailA.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var f logFrameDTO
	_ = json.Unmarshal(data, &f)
	if f.Type != "logs" || len(f.Lines) != 1 || f.Lines[0].Message != "upstream timeout" || f.Lines[0].Labels["ip"] != "[IP]" {
		t.Fatalf("tail frame %s: want only the matching, redacted line", data)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel2()
	if _, data, err := tailB.Read(ctx2); err == nil {
		t.Fatalf("tenant B's tail received tenant A's line: %s", data)
	}
}
