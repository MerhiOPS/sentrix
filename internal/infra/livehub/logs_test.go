package livehub

import (
	"testing"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

func drainLogs(s *LogSub) []LogEvent {
	var out []LogEvent
	for {
		select {
		case e, ok := <-s.Events():
			if !ok {
				return out
			}
			out = append(out, e)
		default:
			return out
		}
	}
}

func TestLiveTailHonoursFiltersAndTenant(t *testing.T) {
	h := New()
	a, b := domain.NewTenantID(), domain.NewTenantID()
	h1, h2 := domain.NewHostID(), domain.NewHostID()
	clauses, _ := query.ParseSearch("timeout")
	errOnlyH1 := h.SubscribeLogs(a, LogFilter{Hosts: []domain.HostID{h1}, Levels: []domain.Level{domain.LevelError}, Clauses: clauses})
	otherTenant := h.SubscribeLogs(b, LogFilter{})
	lines := []domain.LogLine{
		{Level: domain.LevelError, Message: "upstream TIMEOUT"},
		{Level: domain.LevelInfo, Message: "upstream timeout"}, // wrong level
		{Level: domain.LevelError, Message: "disk full"},       // no term match
	}
	h.PublishLogs(a, h1, lines)
	h.PublishLogs(a, h2, lines) // wrong host
	ev := drainLogs(errOnlyH1)
	if len(ev) != 1 || len(ev[0].Lines) != 1 || ev[0].Lines[0].Message != "upstream TIMEOUT" {
		t.Fatalf("filtered tail got %+v", ev)
	}
	if got := drainLogs(otherTenant); len(got) != 0 {
		t.Fatalf("tenant B tail received tenant A's lines: %+v", got)
	}
}

func TestExplicitEmptyHostListTailsNothing(t *testing.T) {
	h := New()
	tn := domain.NewTenantID()
	none := h.SubscribeLogs(tn, LogFilter{Hosts: []domain.HostID{}})
	all := h.SubscribeLogs(tn, LogFilter{})
	h.PublishLogs(tn, domain.NewHostID(), []domain.LogLine{{Message: "x"}})
	if len(drainLogs(none)) != 0 || len(drainLogs(all)) != 1 {
		t.Fatal("hosts [] must mean none and nil must mean all, as on the metrics feed")
	}
}

func TestSlowTailDroppedNotBlocking(t *testing.T) {
	h := New()
	tn := domain.NewTenantID()
	sub := h.SubscribeLogs(tn, LogFilter{})
	for i := 0; i < sendBuffer+5; i++ {
		h.PublishLogs(tn, domain.NewHostID(), []domain.LogLine{{Message: "x"}})
	}
	if h.SlowDropped() != 1 {
		t.Fatalf("slow tail not dropped: %d", h.SlowDropped())
	}
	n := 0
	for range sub.Events() {
		n++
	}
	if n != sendBuffer {
		t.Fatalf("tail got %d events before close", n)
	}
}
