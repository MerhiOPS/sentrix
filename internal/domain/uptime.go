package domain

import "time"

// MonitorHour is one monitor's hourly rollup. Up and Down are the monitored
// time attributed to each state; the rest of the hour was not monitored
// (paused, pending, the server not checking, or unknown results).
type MonitorHour struct {
	Monitor    int64
	Hour       time.Time // UTC, truncated to the hour
	Checks     int
	Failures   int
	Up, Down   time.Duration
	LatencySum time.Duration // over checks that measured one
	LatencyN   int
	LatencyMax time.Duration
}

// HourSlice is the part of a span inside one UTC hour.
type HourSlice struct {
	Hour time.Time
	D    time.Duration
}

// HourSlices splits a span at UTC hour boundaries, so time is attributed to
// the hours it actually fell in.
func HourSlices(s Span) []HourSlice {
	var out []HourSlice
	for t := s.Start.UTC(); t.Before(s.End); {
		h := t.Truncate(time.Hour)
		end := h.Add(time.Hour)
		if s.End.Before(end) {
			end = s.End.UTC()
		}
		out = append(out, HourSlice{Hour: h, D: end.Sub(t)})
		t = end
	}
	return out
}

// Uptime is up / (up + down) over the hours given. ok is false when none of
// that time was monitored: no data is not 100 %.
func Uptime(hours []MonitorHour) (ratio float64, monitored time.Duration, ok bool) {
	var up, down time.Duration
	for _, h := range hours {
		up += max(h.Up, 0)
		down += max(h.Down, 0)
	}
	if up+down <= 0 {
		return 0, 0, false
	}
	return float64(up) / float64(up+down), up + down, true
}

// UptimeWindow is the hours of the window [now-d, now], hour-aligned: the
// first hour is included whole, so a 24 h window covers 24-25 hours.
func UptimeWindow(hours []MonitorHour, now time.Time, d time.Duration) []MonitorHour {
	from := now.Add(-d).UTC().Truncate(time.Hour)
	var out []MonitorHour
	for _, h := range hours {
		if !h.Hour.Before(from) && !h.Hour.After(now) {
			out = append(out, h)
		}
	}
	return out
}

// DayBar is one UTC day of a monitor's history.
type DayBar struct {
	Day      time.Time
	Up, Down time.Duration
	Failures int
}

// DailyBars folds hours into the last days UTC days, oldest first, ending
// with today. Days without monitored time are present with zero durations:
// the UI shows them as gaps, not as up.
func DailyBars(hours []MonitorHour, now time.Time, days int) []DayBar {
	today := now.UTC().Truncate(24 * time.Hour)
	out := make([]DayBar, days)
	for i := range out {
		out[i].Day = today.AddDate(0, 0, i-days+1)
	}
	for _, h := range hours {
		i := days - 1 - int(today.Sub(h.Hour.UTC().Truncate(24*time.Hour))/(24*time.Hour))
		if i < 0 || i >= days {
			continue
		}
		out[i].Up += max(h.Up, 0)
		out[i].Down += max(h.Down, 0)
		out[i].Failures += h.Failures
	}
	return out
}
