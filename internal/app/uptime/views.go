package uptime

import (
	"context"
	"slices"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Uptime windows the dashboard shows.
const (
	Window24h = 24 * time.Hour
	Window7d  = 7 * 24 * time.Hour
	Window30d = 30 * 24 * time.Hour
	Window90d = 90 * 24 * time.Hour
)

// ListBars and DetailBars are how many daily bars each view carries.
const (
	ListBars   = 30
	DetailBars = 90
)

// View is one monitor as the dashboard shows it.
type View struct {
	Monitor domain.Monitor
	State   domain.MonitorState
	// Uptime by window; absent when nothing in the window was monitored.
	Uptime map[time.Duration]float64
	Bars   []domain.DayBar
	// AvgLatency is over the last 24 h of checks that measured one; nil
	// when none did (a measured 0 ms is a value, not missing).
	AvgLatency *time.Duration
}

// List returns every monitor with its state, 24 h and 30 d uptime and the
// last ListBars days, from one read of the hourly rollups.
func (a *Admin) List(ctx context.Context, s domain.Scope) ([]View, error) {
	ms, err := a.d.Monitors.ListMonitors(ctx, s)
	if err != nil {
		return nil, err
	}
	states, err := a.d.States.MonitorStates(ctx, s)
	if err != nil {
		return nil, err
	}
	now := a.d.Now().UTC()
	hours, err := a.d.History.MonitorHours(ctx, s, 0, now.Add(-Window30d).Truncate(time.Hour), now.Add(time.Hour))
	if err != nil {
		return nil, err
	}
	byMonitor := map[int64][]domain.MonitorHour{}
	for _, h := range hours {
		byMonitor[h.Monitor] = append(byMonitor[h.Monitor], h)
	}
	byState := map[int64]domain.MonitorState{}
	for _, st := range states {
		byState[st.Monitor] = st
	}
	out := make([]View, 0, len(ms))
	for _, m := range ms {
		out = append(out, view(m, byState[m.ID], byMonitor[m.ID], now, ListBars, Window24h, Window30d))
	}
	return out, nil
}

// Get returns one monitor with uptime for every window and DetailBars days.
func (a *Admin) Get(ctx context.Context, s domain.Scope, id int64) (View, error) {
	m, err := a.d.Monitors.GetMonitor(ctx, s, id)
	if err != nil {
		return View{}, err
	}
	states, err := a.d.States.MonitorStates(ctx, s)
	if err != nil {
		return View{}, err
	}
	var st domain.MonitorState
	for _, x := range states {
		if x.Monitor == id {
			st = x
		}
	}
	now := a.d.Now().UTC()
	hours, err := a.d.History.MonitorHours(ctx, s, id, now.Add(-Window90d).Truncate(time.Hour), now.Add(time.Hour))
	if err != nil {
		return View{}, err
	}
	return view(m, st, hours, now, DetailBars, Window24h, Window7d, Window30d, Window90d), nil
}

// withOpenSpan adds the time since the last check, in the state it is in,
// to a copy of the rollups. The next check will attribute exactly that
// (within the same two-interval limit), so the view does not show a down
// monitor at 100 % for up to one interval.
func withOpenSpan(m domain.Monitor, st domain.MonitorState, hours []domain.MonitorHour, now time.Time) []domain.MonitorHour {
	if st.LastCheck.IsZero() || (st.Status != domain.MonitorUp && st.Status != domain.MonitorDown) {
		return hours
	}
	end := now
	if limit := st.LastCheck.Add(2 * m.Interval); limit.Before(end) {
		end = limit
	}
	out := append([]domain.MonitorHour(nil), hours...)
	for _, sl := range domain.HourSlices(domain.Span{Start: st.LastCheck, End: end}) {
		i := slices.IndexFunc(out, func(h domain.MonitorHour) bool { return h.Hour.Equal(sl.Hour) })
		if i < 0 {
			out = append(out, domain.MonitorHour{Monitor: m.ID, Hour: sl.Hour})
			i = len(out) - 1
		}
		if st.Status == domain.MonitorDown {
			out[i].Down += sl.D
		} else {
			out[i].Up += sl.D
		}
	}
	return out
}

func view(m domain.Monitor, st domain.MonitorState, hours []domain.MonitorHour, now time.Time, bars int, windows ...time.Duration) View {
	hours = withOpenSpan(m, st, hours, now)
	v := View{Monitor: m, State: st, Uptime: map[time.Duration]float64{}, Bars: domain.DailyBars(hours, now, bars)}
	for _, w := range windows {
		if ratio, _, ok := domain.Uptime(domain.UptimeWindow(hours, now, w)); ok {
			v.Uptime[w] = ratio
		}
	}
	var sum time.Duration
	var n int
	for _, h := range domain.UptimeWindow(hours, now, Window24h) {
		sum += h.LatencySum
		n += h.LatencyN
	}
	if n > 0 {
		avg := sum / time.Duration(n)
		v.AvgLatency = &avg
	}
	return v
}
