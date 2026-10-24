package e2e

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/ingesthttp"
	"github.com/MerhiOPS/sentrix/internal/infra/sqlite"
	"github.com/MerhiOPS/sentrix/internal/wire"
)

// benchHosts are enrolled up front; requests rotate over them so no host
// approaches the per-host rate cap.
const benchHosts = 1000

// typicalBatch is what one host sends every 10 s at a 5 s interval: two core
// samples and, per sample, 8 cores, 2 mounts, 2 disks and 2 NICs (18 detail
// values) — 38 values, gzip JSON, as the agent encodes it.
func typicalBatch(seq uint64, at time.Time) []byte {
	b := wire.Batch{BatchSeq: seq, IntervalMS: 5000}
	for i := range 2 {
		ts := at.Add(time.Duration(i) * 5 * time.Second).UnixMilli()
		b.Samples = append(b.Samples, wire.Sample{TSMS: ts, CPUPercent: 37.5, MemoryPercent: 61.2, DiskPercent: 48,
			NetRxBytes: seq * 1000, NetTxBytes: seq * 400})
		add := func(metric, kind, label, v string, val float64) {
			b.Details = append(b.Details, wire.Detail{TSMS: ts, Metric: metric, Kind: kind, Labels: map[string]string{label: v}, Value: val})
		}
		for c := range 8 {
			add("cpu_core_percent", "gauge", "core", strconv.Itoa(c), 30)
		}
		for _, m := range []string{"/", "/var"} {
			add("disk_used_percent", "gauge", "mount", m, 48)
		}
		for _, d := range []string{"sda", "sdb"} {
			add("disk_read_bytes", "counter", "device", d, float64(seq)*4096)
			add("disk_write_bytes", "counter", "device", d, float64(seq)*8192)
		}
		for _, n := range []string{"eth0", "eth1"} {
			add("net_rx_bytes", "counter", "nic", n, float64(seq)*1500)
			add("net_tx_bytes", "counter", "nic", n, float64(seq)*900)
		}
	}
	raw, _ := json.Marshal(b)
	var z bytes.Buffer
	zw := gzip.NewWriter(&z)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	return z.Bytes()
}

// BenchmarkIngestCeiling measures the whole server ingest path — HTTP
// handler, gzip + JSON decode, credential cache, validation, counter folding,
// rollups, detail series, one SQLite transaction per batch — with requests
// from 8 concurrent clients. hosts@5s is the fleet that rate sustains: each
// host sends one batch per 10 s.
//
//	go test ./internal/e2e -run '^$' -bench IngestCeiling -benchtime 20000x
func BenchmarkIngestCeiling(b *testing.B) {
	ctx := context.Background()
	db, err := sqlite.OpenMigrated(ctx, filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	st := sqlite.NewStore(db)
	tn, err := identity.NewTenants(st, time.Now).Create(ctx, "bench")
	if err != nil {
		b.Fatal(err)
	}
	enr := identity.NewEnrollment(st, time.Now)
	h := ingesthttp.NewHandler(ingest.NewAuthenticator(st, time.Hour, benchHosts*2, time.Now),
		ingest.NewService(st, ingest.DefaultWindow, time.Now), enr)
	sc, _ := domain.NewScope(tn.ID)
	creds := make([]string, benchHosts)
	for i := range creds {
		creds[i] = enrollBench(b, h, enr, sc, i)
	}
	start := time.Now().Add(-60 * time.Hour) // inside the 72 h window, advancing 10 s per batch
	var next atomic.Uint64
	b.ResetTimer()
	t0 := time.Now()
	var wg sync.WaitGroup
	for w := range 8 {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for {
				n := next.Add(1)
				if n > uint64(b.N) {
					return
				}
				host, round := int(n%benchHosts), n/benchHosts
				// Encoding runs on the same machine and in the timed loop, so
				// the figure is conservative.
				body := typicalBatch(n, start.Add(time.Duration(round)*10*time.Second))
				req := httptest.NewRequest(http.MethodPost, wire.PathBatch, bytes.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+creds[host])
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Content-Encoding", "gzip")
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					b.Errorf("worker %d batch %d: %d %s", w, n, rec.Code, rec.Body)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	rate := float64(b.N) / time.Since(t0).Seconds()
	b.ReportMetric(rate, "batches/s")
	b.ReportMetric(rate*10, "hosts@5s")
	b.ReportMetric(rate*38, "values/s")
}

func enrollBench(b *testing.B, h http.Handler, enr *identity.Enrollment, sc domain.Scope, i int) string {
	b.Helper()
	secret, _, err := enr.IssueToken(context.Background(), sc)
	if err != nil {
		b.Fatal(err)
	}
	req := wire.EnrollRequest{Token: wire.Token{Secret: secret}.String(),
		Host: wire.HostInfo{Hostname: fmt.Sprintf("bench-%04d", i), OS: "linux", AgentVersion: "bench", IntervalMS: 5000}}
	raw, _ := json.Marshal(req)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, wire.PathEnroll, bytes.NewReader(raw)))
	var resp wire.EnrollResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &resp) != nil {
		b.Fatalf("enroll %d: %d %s", i, rec.Code, rec.Body)
	}
	return resp.Credential
}
