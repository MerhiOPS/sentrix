package storetest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

func line(sec int, level domain.Level, msg string) domain.LogLine {
	return domain.LogLine{TS: T0.Add(time.Duration(sec) * time.Second), Source: "app", Level: level, Message: msg}
}

// withLogs stores lines; it reports the budget the store offered build.
func withLogs(budget *int64, lines ...domain.LogLine) ingest.BuildFunc {
	return func(st ingest.TxState) (ingest.Prepared, error) {
		if budget != nil {
			*budget = st.LogBudget
		}
		var bytes int64
		for _, l := range lines {
			bytes += int64(l.Size())
		}
		return ingest.Prepared{Logs: lines, LogStats: ingest.LogStats{Received: len(lines), Bytes: bytes},
			Counters: st.Counters, Interval: 5 * time.Second, ReceivedAt: T0, SeqRetention: ingest.DefaultWindow.SeqRetention()}, nil
	}
}

func search(t *testing.T, st Stores, f fixture, q string, mutate ...func(*query.LogQuery)) []query.LogEntry {
	t.Helper()
	clauses, err := query.ParseSearch(q)
	if err != nil {
		t.Fatal(err)
	}
	lq := query.LogQuery{Clauses: clauses, From: T0.Add(-time.Hour), To: T0.Add(time.Hour), Limit: 100}
	for _, m := range mutate {
		m(&lq)
	}
	out, err := st.SearchLogs(context.Background(), f.scope, lq)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func messages(es []query.LogEntry) string {
	s := ""
	for _, e := range es {
		s += "[" + e.Line.Message + "]"
	}
	return s
}

func testLogSearchSemantics(t *testing.T, st Stores) {
	f := newFixture(t, st, "a")
	commit(t, st, f, 1, withLogs(nil,
		line(1, domain.LevelError, "Connection REFUSED by upstream"),
		line(2, domain.LevelWarn, "read timeout after 30s"),
		line(3, domain.LevelInfo, "connection reset by peer"),
		line(4, domain.LevelInfo, `odd: title:xyz NEAR(abc) *glob ^caret -minus AND "q"`),
	))
	for q, want := range map[string]string{
		"refused":              "[Connection REFUSED by upstream]",
		"CONNECTION":           "[connection reset by peer][Connection REFUSED by upstream]",
		"connection upstream":  "[Connection REFUSED by upstream]",
		"refused OR timeout":   "[read timeout after 30s][Connection REFUSED by upstream]",
		`"reset by peer"`:      "[connection reset by peer]",
		`"by peer" connection`: "[connection reset by peer]",
		// FTS5 syntax in user input is plain text (PLAN §4.2).
		"title:xyz":  "[" + `odd: title:xyz NEAR(abc) *glob ^caret -minus AND "q"` + "]",
		"NEAR(abc)":  "[" + `odd: title:xyz NEAR(abc) *glob ^caret -minus AND "q"` + "]",
		"*glob":      "[" + `odd: title:xyz NEAR(abc) *glob ^caret -minus AND "q"` + "]",
		"-minus AND": "[" + `odd: title:xyz NEAR(abc) *glob ^caret -minus AND "q"` + "]",
		"absent":     "",
	} {
		if got := messages(search(t, st, f, q)); got != want {
			t.Errorf("search %q\n got  %s\n want %s", q, got, want)
		}
	}
	lv := func(q *query.LogQuery) { q.Levels = []domain.Level{domain.LevelError, domain.LevelWarn} }
	if got := messages(search(t, st, f, "", lv)); got != "[read timeout after 30s][Connection REFUSED by upstream]" {
		t.Errorf("level filter: %s", got)
	}
	win := func(q *query.LogQuery) { q.From, q.To = T0.Add(2*time.Second), T0.Add(3*time.Second) }
	if got := messages(search(t, st, f, "", win)); got != "[read timeout after 30s]" {
		t.Errorf("time window [from, to): %s", got)
	}
}

func testLogPaginationNewestFirstNoGapsNoDuplicates(t *testing.T, st Stores) {
	f := newFixture(t, st, "a")
	var ls []domain.LogLine
	for i := 0; i < 7; i++ {
		ls = append(ls, line(i/3, domain.LevelInfo, fmt.Sprintf("line-%d", i))) // three per second: ties
	}
	commit(t, st, f, 1, withLogs(nil, ls...))
	var seen []string
	var before *query.LogCursor
	for page := 0; page < 5; page++ {
		got := search(t, st, f, "", func(q *query.LogQuery) { q.Limit, q.Before = 3, before })
		for _, e := range got {
			seen = append(seen, e.Line.Message)
		}
		if len(got) < 3 {
			break
		}
		last := got[len(got)-1]
		before = &query.LogCursor{TS: last.Line.TS, ID: last.ID}
	}
	want := "line-6 line-5 line-4 line-3 line-2 line-1 line-0"
	if got := fmt.Sprint(seen); got != "["+want+"]" {
		t.Fatalf("pages gave %v, want newest first with no gap or duplicate: %s", seen, want)
	}
}

func testLogBudgetReadInTransaction(t *testing.T, st Stores) {
	f := newFixture(t, st, "a")
	if err := st.SetLogLimit(context.Background(), mustOps(t), f.tenant, 100); err != nil {
		t.Fatal(err)
	}
	first := line(0, domain.LevelInfo, "0123456789012345678901234567890123456789") // 40 + 3 bytes
	var b1, b2 int64
	commit(t, st, f, 1, withLogs(&b1, first))
	commit(t, st, f, 2, withLogs(&b2))
	if b1 != 100 || b2 != 100-int64(first.Size()) {
		t.Fatalf("budgets offered %d then %d; want 100 then %d", b1, b2, 100-int64(first.Size()))
	}
}

func testAllDroppedBatchIsMetered(t *testing.T, st Stores) {
	f := newFixture(t, st, "a")
	r := commit(t, st, f, 1, func(s ingest.TxState) (ingest.Prepared, error) {
		return ingest.Prepared{LogStats: ingest.LogStats{Received: 5, Dropped: 5}, Counters: s.Counters,
			Interval: 5 * time.Second, ReceivedAt: T0, SeqRetention: ingest.DefaultWindow.SeqRetention()}, nil
	})
	if r.LogsStored != 0 || r.Replayed {
		t.Fatalf("result %+v", r)
	}
	if got := search(t, st, f, ""); len(got) != 0 {
		t.Fatalf("a fully dropped batch stored lines: %v", messages(got))
	}
}
