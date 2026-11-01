package storetest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/uptime"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// seedMonitor gives f a channel and a monitor through one incident cycle:
// results, hours, an ended incident and two events.
func seedMonitor(ctx context.Context, st Stores, f fixture) (domain.Monitor, int64, error) {
	ch, err := st.CreateChannel(ctx, f.scope, domain.Channel{Name: "ops", Kind: domain.ChannelSlack, URL: "https://hooks.example/a"})
	if err != nil {
		return domain.Monitor{}, 0, err
	}
	id, err := st.CreateMonitor(ctx, f.scope, monitor(ch), 100)
	if err != nil {
		return domain.Monitor{}, 0, err
	}
	m, err := st.GetMonitor(ctx, f.scope, id)
	if err != nil {
		return m, 0, err
	}
	for _, r := range []domain.CheckResult{result(0, domain.CheckUp, time.Millisecond), result(time.Minute, domain.CheckDown, 0),
		result(70*time.Second, domain.CheckDown, 0), result(2*time.Minute, domain.CheckUp, time.Millisecond)} {
		states, err := st.MonitorStates(ctx, f.scope)
		if err != nil {
			return m, 0, fmt.Errorf("seed state: %w", err)
		}
		if len(states) == 0 {
			return m, 0, errors.New("seed state: no monitor state")
		}
		prev := states[len(states)-1]
		if _, err := st.RecordCheck(ctx, f.scope, uptime.CheckRecord{Monitor: m, Result: r,
			Step: domain.StepMonitor(m, prev, r), Revision: prev.Revision}); err != nil {
			return m, 0, err
		}
	}
	return m, ch, nil
}

// aUnchanged reports how A's monitor differs from before, or "".
func aUnchanged(ctx context.Context, st Stores, a fixture, before domain.Monitor) string {
	got, err := st.GetMonitor(ctx, a.scope, before.ID)
	if err != nil {
		return "A's monitor is gone: " + err.Error()
	}
	if got.Name != before.Name || got.Target != before.Target || got.Paused != before.Paused {
		return fmt.Sprintf("A's monitor changed: %+v", got)
	}
	return ""
}

// monitorCrossCases extend crossCases for the uptime ports. B acting on
// A's ids must find nothing and change nothing.
var monitorCrossCases = map[string]crossCase{
	"MonitorRepo.ListMonitors": func(ctx context.Context, st Stores, a, b fixture) string {
		m, _, err := seedMonitor(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		for _, got := range must(st.ListMonitors(ctx, b.scope)) {
			if got.ID == m.ID {
				return "ListMonitors(B) returned A's monitor"
			}
		}
		return ""
	},
	"MonitorRepo.GetMonitor": func(ctx context.Context, st Stores, a, b fixture) string {
		m, _, err := seedMonitor(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		return notFound("GetMonitor(B, A's monitor)", second(st.GetMonitor(ctx, b.scope, m.ID)))
	},
	"MonitorRepo.CreateMonitor": func(ctx context.Context, st Stores, a, b fixture) string {
		_, ch, err := seedMonitor(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		if p := notFound("CreateMonitor(B, A's channel)", second(st.CreateMonitor(ctx, b.scope, monitor(ch), 100))); p != "" {
			return p
		}
		if n := len(must(st.ListMonitors(ctx, b.scope))); n != 0 {
			return "a refused cross-tenant monitor was stored"
		}
		// A's monitors do not count against B's quota.
		if _, err := st.CreateMonitor(ctx, b.scope, monitor(), 1); err != nil {
			return "CreateMonitor(B) on a plan of one, with A holding a monitor: " + err.Error()
		}
		return ""
	},
	"MonitorRepo.UpdateMonitor": func(ctx context.Context, st Stores, a, b fixture) string {
		m, ch, err := seedMonitor(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		hijack := m
		hijack.Name, hijack.Target = "hijacked", "https://evil.example/"
		if p := notFound("UpdateMonitor(B, A's monitor)", st.UpdateMonitor(ctx, b.scope, hijack, T0)); p != "" {
			return p
		}
		mine, _ := st.CreateMonitor(ctx, b.scope, monitor(), 100)
		own := monitor(ch)
		own.ID = mine
		if p := notFound("UpdateMonitor(B, A's channel)", st.UpdateMonitor(ctx, b.scope, own, T0)); p != "" {
			return p
		}
		return aUnchanged(ctx, st, a, m)
	},
	"MonitorRepo.DeleteMonitor": func(ctx context.Context, st Stores, a, b fixture) string {
		m, _, err := seedMonitor(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		if p := notFound("DeleteMonitor(B, A's monitor)", st.DeleteMonitor(ctx, b.scope, m.ID)); p != "" {
			return p
		}
		return aUnchanged(ctx, st, a, m)
	},
	"MonitorRepo.SetPaused": func(ctx context.Context, st Stores, a, b fixture) string {
		m, _, err := seedMonitor(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		if p := notFound("SetPaused(B, A's monitor)", st.SetPaused(ctx, b.scope, m.ID, true, T0)); p != "" {
			return p
		}
		return aUnchanged(ctx, st, a, m)
	},
	"StateRepo.MonitorStates": func(ctx context.Context, st Stores, a, b fixture) string {
		m, _, err := seedMonitor(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		for _, got := range must(st.MonitorStates(ctx, b.scope)) {
			if got.Monitor == m.ID {
				return "MonitorStates(B) returned A's state"
			}
		}
		return ""
	},
	"StateRepo.RecordCheck": func(ctx context.Context, st Stores, a, b fixture) string {
		m, _, err := seedMonitor(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		before := must(st.MonitorStates(ctx, a.scope))[0]
		r := result(time.Hour, domain.CheckDown, 0)
		_, err = st.RecordCheck(ctx, b.scope, uptime.CheckRecord{Monitor: m, Result: r,
			Step: domain.StepMonitor(m, before, r), Revision: before.Revision})
		if !errors.Is(err, uptime.ErrStale) && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Sprintf("RecordCheck(B, A's monitor) err = %v, want ErrStale or ErrNotFound", err)
		}
		if after := must(st.MonitorStates(ctx, a.scope))[0]; after != before {
			return fmt.Sprintf("RecordCheck(B) changed A's state: %+v", after)
		}
		if n := len(must(st.MonitorResults(ctx, a.scope, m.ID, T0, T0.Add(24*time.Hour), 100))); n != 4 {
			return fmt.Sprintf("RecordCheck(B) wrote a result into A's monitor: %d results", n)
		}
		return ""
	},
	"StateRepo.SetMonitorDelivery": func(ctx context.Context, st Stores, a, b fixture) string {
		m, _, err := seedMonitor(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		ev := must(st.ListMonitorEvents(ctx, a.scope, m.ID, 1))[0]
		if p := notFound("SetMonitorDelivery(B, A's event)", st.SetMonitorDelivery(ctx, b.scope, ev.ID, "failed", "x")); p != "" {
			return p
		}
		if got := must(st.ListMonitorEvents(ctx, a.scope, m.ID, 1))[0]; got.Outcome != ev.Outcome {
			return "SetMonitorDelivery(B) changed A's event"
		}
		return ""
	},
	"StateRepo.ListMonitorEvents": func(ctx context.Context, st Stores, a, b fixture) string {
		m, _, err := seedMonitor(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		for _, ev := range must(st.ListMonitorEvents(ctx, b.scope, 0, 1000)) {
			if ev.Monitor == m.ID {
				return "ListMonitorEvents(B, all) returned A's events"
			}
		}
		if got := must(st.ListMonitorEvents(ctx, b.scope, m.ID, 100)); len(got) != 0 {
			return "ListMonitorEvents(B, A's monitor) returned A's events"
		}
		return ""
	},
	"HistoryReader.MonitorResults": func(ctx context.Context, st Stores, a, b fixture) string {
		m, _, err := seedMonitor(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		if got := must(st.MonitorResults(ctx, b.scope, m.ID, T0, T0.Add(24*time.Hour), 100)); len(got) != 0 {
			return "MonitorResults(B, A's monitor) returned A's results"
		}
		return ""
	},
	"HistoryReader.MonitorHours": func(ctx context.Context, st Stores, a, b fixture) string {
		m, _, err := seedMonitor(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		for _, id := range []int64{0, m.ID} {
			for _, h := range must(st.MonitorHours(ctx, b.scope, id, T0, T0.Add(24*time.Hour))) {
				if h.Monitor == m.ID {
					return fmt.Sprintf("MonitorHours(B, %d) returned A's hours", id)
				}
			}
		}
		return ""
	},
	"HistoryReader.MonitorIncidents": func(ctx context.Context, st Stores, a, b fixture) string {
		m, _, err := seedMonitor(ctx, st, a)
		if err != nil {
			return err.Error()
		}
		if got := must(st.MonitorIncidents(ctx, b.scope, m.ID, 100)); len(got) != 0 {
			return "MonitorIncidents(B, A's monitor) returned A's incidents"
		}
		return ""
	},
}
