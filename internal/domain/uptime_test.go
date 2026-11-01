package domain

import (
	"math"
	"testing"
	"time"
)

func TestHourSlicesSplitAtHourBoundaries(t *testing.T) {
	s := Span{Start: m0.Add(-10 * time.Minute), End: m0.Add(70 * time.Minute)} // 11:50 → 13:10
	got := HourSlices(s)
	want := []HourSlice{{m0.Add(-time.Hour), 10 * time.Minute}, {m0, time.Hour}, {m0.Add(time.Hour), 10 * time.Minute}}
	if len(got) != len(want) {
		t.Fatalf("slices %v, want %v", got, want)
	}
	for i := range want {
		if !got[i].Hour.Equal(want[i].Hour) || got[i].D != want[i].D {
			t.Fatalf("slice %d = %v, want %v", i, got[i], want[i])
		}
	}
	if HourSlices(Span{m0, m0}) != nil {
		t.Fatal("an empty span has no slices")
	}
}

func TestUptimeExcludesUnmonitoredTime(t *testing.T) {
	// Hand-computed: 3 h window; hour 1 fully up, hour 2 has a 6 min
	// incident and 30 min paused, hour 3 has a 15 min server-side gap.
	hours := []MonitorHour{
		{Hour: m0, Up: time.Hour},
		{Hour: m0.Add(time.Hour), Up: 24 * time.Minute, Down: 6 * time.Minute},
		{Hour: m0.Add(2 * time.Hour), Up: 45 * time.Minute},
	}
	ratio, monitored, ok := Uptime(hours)
	want := float64(129) / float64(135) // 129 min up of 135 monitored
	if !ok || monitored != 135*time.Minute || math.Abs(ratio-want) > 1e-12 {
		t.Fatalf("uptime %v over %s (ok %v), want %v over 135m", ratio, monitored, ok, want)
	}
	if _, _, ok := Uptime([]MonitorHour{{Hour: m0}}); ok {
		t.Fatal("no monitored time must not read as 100 %")
	}
	if r, _, _ := Uptime([]MonitorHour{{Up: time.Hour, Down: -time.Minute}}); r != 1 {
		t.Fatalf("a negative bucket (clamped reclassification) must count as zero, got %v", r)
	}
	if r, _, _ := Uptime([]MonitorHour{{Up: -time.Minute, Down: time.Hour}}); r != 0 {
		t.Fatalf("a negative up bucket must count as zero, got %v", r)
	}
}

func TestUptimeWindowIsHourAligned(t *testing.T) {
	now := m0.Add(30 * time.Minute) // 12:30
	hours := []MonitorHour{{Hour: m0.Add(-25 * time.Hour)}, {Hour: m0.Add(-24 * time.Hour)}, {Hour: m0}, {Hour: m0.Add(time.Hour)}}
	got := UptimeWindow(hours, now, 24*time.Hour)
	if len(got) != 2 || !got[0].Hour.Equal(m0.Add(-24*time.Hour)) || !got[1].Hour.Equal(m0) {
		t.Fatalf("window %v: want the hour containing now-24h through the current hour", got)
	}
}

func TestDailyBarsKeepEmptyDays(t *testing.T) {
	now := m0
	hours := []MonitorHour{
		{Hour: m0.Add(-time.Hour), Up: 50 * time.Minute, Down: 10 * time.Minute, Failures: 3},
		{Hour: m0, Up: time.Hour},
		{Hour: m0.Add(-48 * time.Hour), Up: time.Hour},
		{Hour: m0.Add(-90 * 24 * time.Hour), Up: time.Hour}, // outside the 3 days
	}
	bars := DailyBars(hours, now, 3)
	if len(bars) != 3 || !bars[2].Day.Equal(m0.Truncate(24*time.Hour)) {
		t.Fatalf("bars %v", bars)
	}
	if bars[2].Up != 110*time.Minute || bars[2].Down != 10*time.Minute || bars[2].Failures != 3 {
		t.Fatalf("today %+v", bars[2])
	}
	if bars[1].Up != 0 || bars[1].Down != 0 {
		t.Fatalf("yesterday had no monitored time and must stay empty: %+v", bars[1])
	}
	if bars[0].Up != time.Hour {
		t.Fatalf("two days ago %+v", bars[0])
	}
}
