package ingesthttp

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/memstore"
	"github.com/MerhiOPS/sentrix/internal/wire"
)

var now0 = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

type rig struct {
	store  *memstore.Store
	h      http.Handler
	enroll *identity.Enrollment
	scope  domain.Scope
}

func newRig(t *testing.T) *rig {
	t.Helper()
	st := memstore.New()
	clock := func() time.Time { return now0 }
	tn, err := identity.NewTenants(st, clock).Create(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	sc, _ := domain.NewScope(tn.ID)
	enr := identity.NewEnrollment(st, clock)
	auth := ingest.NewAuthenticator(st, time.Minute, 100, clock)
	svc := ingest.NewService(st, ingest.DefaultWindow, clock)
	return &rig{store: st, h: NewHandler(auth, svc, enr), enroll: enr, scope: sc}
}

func (r *rig) token(t *testing.T) string {
	t.Helper()
	secret, _, err := r.enroll.IssueToken(context.Background(), r.scope)
	if err != nil {
		t.Fatal(err)
	}
	return wire.Token{Secret: secret}.String()
}

func do(t *testing.T, h http.Handler, path, cred string, body []byte, gz bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	if cred != "" {
		req.Header.Set("Authorization", "Bearer "+cred)
	}
	if gz {
		req.Header.Set("Content-Encoding", "gzip")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func gzipped(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (r *rig) enrolled(t *testing.T) (cred string, host wire.EnrollResponse) {
	t.Helper()
	req := wire.EnrollRequest{Token: r.token(t), Host: wire.HostInfo{
		Hostname: "web-1", OS: "linux", AgentVersion: "0.1.0", IntervalMS: 5000}}
	rec := do(t, r.h, wire.PathEnroll, "", mustJSON(t, req), false)
	if rec.Code != http.StatusOK {
		t.Fatalf("enroll: %d %s", rec.Code, rec.Body)
	}
	var resp wire.EnrollResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Credential, resp
}

func batchBody(t *testing.T, seq uint64, n int) []byte {
	t.Helper()
	b := wire.Batch{BatchSeq: seq, IntervalMS: 5000}
	for i := 0; i < n; i++ {
		b.Samples = append(b.Samples, wire.Sample{TSMS: now0.Add(time.Duration(i-n) * 5 * time.Second).UnixMilli(), CPUPercent: 10})
	}
	return mustJSON(t, b)
}

func TestEnrollThenBatchStoresUnderCredentialHost(t *testing.T) {
	r := newRig(t)
	cred, er := r.enrolled(t)
	// A forged host_id (and tenant) in the body must not move the rows.
	forged := strings.Replace(string(batchBody(t, 1, 3)), `{"batch_seq"`,
		`{"host_id":"`+domain.NewHostID().String()+`","tenant_id":"`+domain.NewTenantID().String()+`","batch_seq"`, 1)
	rec := do(t, r.h, wire.PathBatch, cred, gzipped(t, []byte(forged)), true)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch: %d %s", rec.Code, rec.Body)
	}
	var ack wire.Ack
	_ = json.Unmarshal(rec.Body.Bytes(), &ack)
	if ack.BatchSeq != 1 || ack.Replayed || ack.ServerTSMS != now0.UnixMilli() {
		t.Fatalf("ack = %+v", ack)
	}
	hid, _ := domain.ParseHostID(er.HostID)
	ss, err := r.store.RawCore(context.Background(), r.scope, hid, now0.Add(-time.Hour), now0.Add(time.Hour))
	if err != nil || len(ss) != 3 {
		t.Fatalf("rows under the credential's host: %d, %v", len(ss), err)
	}
}

func TestReplayIsAckedNotReapplied(t *testing.T) {
	r := newRig(t)
	cred, _ := r.enrolled(t)
	for i, want := range []bool{false, true} {
		rec := do(t, r.h, wire.PathBatch, cred, batchBody(t, 42, 2), false)
		var ack wire.Ack
		_ = json.Unmarshal(rec.Body.Bytes(), &ack)
		if rec.Code != http.StatusOK || ack.Replayed != want {
			t.Fatalf("send %d: %d replayed=%v", i, rec.Code, ack.Replayed)
		}
	}
}

func TestGzipBombRejected(t *testing.T) {
	r := newRig(t)
	cred, _ := r.enrolled(t)
	bomb := gzipped(t, bytes.Repeat([]byte(" "), wire.MaxDecompressedBytes+1)) // ~8 KiB compressed
	if len(bomb) > wire.MaxCompressedBytes {
		t.Fatalf("bomb is %d bytes compressed; the test must stay under the compressed cap", len(bomb))
	}
	if rec := do(t, r.h, wire.PathBatch, cred, bomb, true); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("gzip bomb: %d %s, want 413", rec.Code, rec.Body)
	}
	big := bytes.Repeat([]byte("x"), wire.MaxCompressedBytes+1)
	if rec := do(t, r.h, wire.PathBatch, cred, big, false); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: %d, want 413", rec.Code)
	}
}

func TestStatusContract(t *testing.T) {
	r := newRig(t)
	cred, _ := r.enrolled(t)
	cases := []struct {
		name string
		cred string
		body []byte
		want int
	}{
		{"no credential", "", batchBody(t, 1, 1), http.StatusUnauthorized},
		{"unknown credential", wire.EncodeSecret(identity.NewSecret()), batchBody(t, 1, 1), http.StatusUnauthorized},
		{"malformed json", cred, []byte("{nope"), http.StatusBadRequest},
		{"seq zero", cred, batchBody(t, 0, 1), http.StatusUnprocessableEntity},
		{"too many samples", cred, batchBody(t, 2, wire.MaxSamplesPerBatch+1), http.StatusRequestEntityTooLarge},
	}
	for _, c := range cases {
		if rec := do(t, r.h, wire.PathBatch, c.cred, c.body, false); rec.Code != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, rec.Code, rec.Body, c.want)
		}
	}
}

func TestTokenReuseRejected(t *testing.T) {
	r := newRig(t)
	tok := r.token(t)
	body := mustJSON(t, wire.EnrollRequest{Token: tok, Host: wire.HostInfo{Hostname: "h", OS: "linux", IntervalMS: 5000}})
	if rec := do(t, r.h, wire.PathEnroll, "", body, false); rec.Code != http.StatusOK {
		t.Fatalf("first use: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, r.h, wire.PathEnroll, "", body, false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("reuse: %d %s, want 401", rec.Code, rec.Body)
	}
}

// failingIngester simulates a store failure: the agent must see a retryable
// 5xx with no internal detail.
type failingIngester struct{}

func (failingIngester) Ingest(context.Context, ingest.AuthHost, ingest.Batch) (ingest.Result, error) {
	return ingest.Result{}, errors.New("disk I/O error at /var/lib/sentrix/s.db")
}

func TestStoreFailureIs500WithoutDetail(t *testing.T) {
	r := newRig(t)
	cred, _ := r.enrolled(t)
	auth := ingest.NewAuthenticator(r.store, time.Minute, 10, func() time.Time { return now0 })
	h := NewHandler(auth, failingIngester{}, r.enroll)
	rec := do(t, h, wire.PathBatch, cred, batchBody(t, 1, 1), false)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "/var/lib") {
		t.Fatalf("store failure: %d %s", rec.Code, rec.Body)
	}
}

func TestCheckListen(t *testing.T) {
	for addr, ok := range map[string]bool{
		"127.0.0.1:8443": true, "[::1]:8443": true, "localhost:8443": true,
		":8443": false, "0.0.0.0:8443": false, "10.0.0.5:8443": false, "example.com:80": false,
	} {
		err := CheckListen(addr, true)
		if (err == nil) != ok {
			t.Errorf("CheckListen(%q, insecure) = %v, want ok=%v", addr, err, ok)
		}
	}
	if err := CheckListen("0.0.0.0:8443", false); err != nil {
		t.Errorf("TLS listen refused: %v", err)
	}
}
