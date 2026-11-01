package uptime

import (
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

func TestViewCountsTheTimeSinceTheLastCheck(t *testing.T) {
	m := domain.Monitor{ID: 1, Interval: time.Minute}
	now := t0.Add(30 * time.Minute)
	hours := []domain.MonitorHour{{Monitor: 1, Hour: t0, Up: 29 * time.Minute}}
	down := domain.MonitorState{Status: domain.MonitorDown, LastCheck: now.Add(-time.Minute)}
	v := view(m, down, hours, now, 1, Window24h)
	if got, want := v.Uptime[Window24h], 29.0/30.0; got != want {
		t.Fatalf("uptime %v, want %v: the minute since the failed check counts as down", got, want)
	}
	if hours[0].Down != 0 {
		t.Fatal("the stored rollup was modified")
	}
	stale := domain.MonitorState{Status: domain.MonitorDown, LastCheck: now.Add(-time.Hour)}
	if got := view(m, stale, nil, now, 1, Window24h).Uptime[Window24h]; got != 0 {
		t.Fatalf("uptime %v: only two intervals after the last check are counted", got)
	}
	if v := view(m, domain.MonitorState{Status: domain.MonitorPaused, LastCheck: now.Add(-time.Minute)}, nil, now, 1, Window24h); len(v.Uptime) != 0 {
		t.Fatalf("a paused monitor's idle time counted: %v", v.Uptime)
	}
}
