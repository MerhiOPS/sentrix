package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/app/retention"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// testRetentionPrunesOnlyOldRowsOfItsKind: pruning raw keeps the rollups
// (charts switch to them), batches are bounded by limit, pruned log lines
// leave search, and a target naming another tenant's host is not found.
func testRetentionPrunesOnlyOldRowsOfItsKind(t *testing.T, st Stores) {
	ctx, ops := context.Background(), mustOps(t)
	f := newFixture(t, st, "a")
	twoHours := 1440 // in 5 s intervals
	if _, err := st.CommitBatch(ctx, f.scope, f.host, 1, prepared(T0, []int{0, 1, 2, twoHours}, []uint64{1, 2, 3, 4})); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CommitBatch(ctx, f.scope, f.host, 2, withLogs(nil, line(0, domain.LevelInfo, "ancient-line"),
		line(2*3600, domain.LevelInfo, "recent-line"))); err != nil {
		t.Fatal(err)
	}
	d := detail(0, domain.MetricCPUCore, domain.KindGauge, domain.Labels{"core": "0"}, 5)
	if _, err := st.CommitBatch(ctx, f.scope, f.host, 3, withDetails(ingest.DefaultWindow.SeqRetention(), d)); err != nil {
		t.Fatal(err)
	}
	tg := retention.Target{Tenant: f.tenant, Host: f.host}
	cut := T0.Add(time.Hour)
	// A second host of the same tenant with the same old data: pruning the
	// first must not touch it (every delete is per host).
	sibling, _ := enroll(t, st, f.scope, "sibling")
	if _, err := st.CommitBatch(ctx, f.scope, sibling.ID, 9, prepared(T0, []int{0, 1}, []uint64{1, 2})); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CommitBatch(ctx, f.scope, sibling.ID, 10, withLogs(nil, line(0, domain.LevelInfo, "sibling-ancient"))); err != nil {
		t.Fatal(err)
	}
	sd := detail(0, domain.MetricCPUCore, domain.KindGauge, domain.Labels{"core": "0"}, 7)
	if _, err := st.CommitBatch(ctx, f.scope, sibling.ID, 11, withDetails(ingest.DefaultWindow.SeqRetention(), sd)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		raw, _ := st.RawCore(ctx, f.scope, sibling.ID, T0.Add(-time.Hour), T0.Add(3*time.Hour))
		rs, _ := st.Rollups(ctx, f.scope, sibling.ID, query.Res1m, T0.Add(-time.Hour), T0.Add(3*time.Hour))
		clauses, _ := query.ParseSearch("sibling-ancient")
		logs, _ := st.SearchLogs(ctx, f.scope, query.LogQuery{Clauses: clauses, From: T0.Add(-time.Hour), To: T0.Add(time.Hour), Limit: 10})
		_, hasDetail, _ := st.OldestHost(ctx, ops, retention.Target{Tenant: f.tenant, Host: sibling.ID}, domain.DataDetail)
		if len(raw) != 2 || len(rs) != 1 || len(logs) != 1 || !hasDetail {
			t.Errorf("pruning one host touched its sibling: raw %d rollups %d logs %d details %v", len(raw), len(rs), len(logs), hasDetail)
		}
	}()

	if n, err := st.PruneHost(ctx, ops, tg, domain.DataRawCore, cut, 2); err != nil || n != 2 {
		t.Fatalf("first raw batch: %d, %v; want the limit of 2", n, err)
	}
	if n, _ := st.PruneHost(ctx, ops, tg, domain.DataRawCore, cut, 2); n != 1 {
		t.Fatalf("second raw batch: %d, want the 1 remaining old sample", n)
	}
	raw, _ := st.RawCore(ctx, f.scope, f.host, T0.Add(-time.Hour), T0.Add(3*time.Hour))
	if len(raw) != 1 || !raw[0].TS.Equal(T0.Add(2*time.Hour)) {
		t.Fatalf("raw after pruning: %d samples, want only the recent one", len(raw))
	}
	if rs, _ := st.Rollups(ctx, f.scope, f.host, query.Res1m, T0.Add(-time.Hour), T0.Add(3*time.Hour)); len(rs) != 2 {
		t.Fatalf("1m rollups after pruning raw: %d buckets, want both kept", len(rs))
	}
	if _, err := st.PruneHost(ctx, ops, tg, domain.DataRollup1m, cut, 1000); err != nil {
		t.Fatal(err)
	}
	if rs, _ := st.Rollups(ctx, f.scope, f.host, query.Res1m, T0.Add(-time.Hour), T0.Add(3*time.Hour)); len(rs) != 1 {
		t.Fatalf("1m rollups after pruning them: %d, want the recent bucket", len(rs))
	}
	if rs, _ := st.Rollups(ctx, f.scope, f.host, query.Res1h, T0.Add(-time.Hour), T0.Add(3*time.Hour)); len(rs) != 2 {
		t.Fatalf("1h rollups after pruning 1m: %d, want both kept", len(rs))
	}
	for _, k := range []domain.DataKind{domain.DataLogs, domain.DataDetail} {
		if n, err := st.PruneHost(ctx, ops, tg, k, cut, 1000); err != nil || n != 1 {
			t.Fatalf("prune %s: %d, %v; want 1", k, n, err)
		}
	}
	for q, want := range map[string]int{"ancient-line": 0, "recent-line": 1} {
		clauses, _ := query.ParseSearch(q)
		got, _ := st.SearchLogs(ctx, f.scope, query.LogQuery{Clauses: clauses, From: T0.Add(-time.Hour), To: T0.Add(3 * time.Hour), Limit: 10})
		if len(got) != want {
			t.Fatalf("search %q after pruning logs: %d hits, want %d", q, len(got), want)
		}
	}
	if oldest, ok, err := st.OldestHost(ctx, ops, tg, domain.DataRawCore); err != nil || !ok || !oldest.Equal(T0.Add(2*time.Hour)) {
		t.Fatalf("oldest raw = %v %v %v", oldest, ok, err)
	}
	if _, ok, _ := st.OldestHost(ctx, ops, tg, domain.DataDetail); ok {
		t.Fatal("details remain after pruning all of them")
	}
	other := newFixture(t, st, "b")
	if _, err := st.PruneHost(ctx, ops, retention.Target{Tenant: other.tenant, Host: f.host}, domain.DataRawCore, T0.Add(24*time.Hour), 1000); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a target pairing tenant B with A's host: %v, want ErrNotFound", err)
	}
	if raw, _ := st.RawCore(ctx, f.scope, f.host, T0.Add(-time.Hour), T0.Add(3*time.Hour)); len(raw) != 1 {
		t.Fatal("a mismatched target deleted rows")
	}
	if err := st.Compact(ctx, ops); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PruneHost(ctx, domain.OpsScope{}, tg, domain.DataRawCore, cut, 1); err == nil {
		t.Fatal("PruneHost accepted a zero OpsScope")
	}
}
