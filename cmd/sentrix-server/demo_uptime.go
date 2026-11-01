package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/app/uptime"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/probe"
	"github.com/MerhiOPS/sentrix/internal/infra/sqlite"
)

// The demo's monitors check services it runs itself on loopback, so the
// demo shows a full incident cycle without touching the internet.
const (
	demoCheckEvery = 30 * time.Second
	// The payments API is down for demoDownMinutes of every demoCycle.
	demoCycle       = 10 * time.Minute
	demoDownMinutes = 2 * time.Minute
)

type demoMonitor struct {
	name string
	kind domain.MonitorKind
	// target is filled in once the service listens.
	target  string
	keyword string
}

// startDemoTargets serves the demo's services until ctx ends and returns
// the monitors that check them.
func startDemoTargets(ctx context.Context) ([]demoMonitor, error) {
	pages := map[string]http.HandlerFunc{
		"Storefront": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<h1>Demo shop</h1><button>Add to cart</button>"))
		},
		"Payments API": func(w http.ResponseWriter, _ *http.Request) {
			if time.Duration(time.Now().UnixNano())%demoCycle < demoDownMinutes {
				http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		},
		"Status page": func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(time.Duration(40+demoJitter(120)) * time.Millisecond) // a believable response-time curve
			_, _ = w.Write([]byte("all systems operational"))
		},
	}
	var out []demoMonitor
	var lc net.ListenConfig
	for _, name := range []string{"Storefront", "Payments API", "Status page"} {
		ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		srv := &http.Server{Handler: pages[name], ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = srv.Serve(ln) }()
		go func() { <-ctx.Done(); _ = srv.Close() }()
		m := demoMonitor{name: name, kind: domain.MonitorHTTP, target: "http://" + ln.Addr().String() + "/"}
		if name == "Storefront" {
			m.keyword = "Add to cart"
		}
		out = append(out, m)
	}
	db, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	go acceptAndClose(ctx, db)
	out = append(out, demoMonitor{name: "Database port", kind: domain.MonitorTCP, target: db.Addr().String()})
	if probe.PingAvailable() == nil {
		out = append(out, demoMonitor{name: "Gateway (ping)", kind: domain.MonitorPing, target: "127.0.0.1"})
	}
	return out, nil
}

func acceptAndClose(ctx context.Context, ln net.Listener) {
	go func() { <-ctx.Done(); _ = ln.Close() }()
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		_ = c.Close()
	}
}

// syncDemoMonitors creates the demo's monitors through the uptime use case,
// or points existing ones at this run's ports.
func syncDemoMonitors(ctx context.Context, st *sqlite.Store, sc domain.Scope, targets []demoMonitor) error {
	tid, err := sc.Tenant()
	if err != nil {
		return err
	}
	plans := identity.NewPlans(st, time.Now)
	plan, _, err := plans.Get(ctx, sc)
	if err != nil {
		return err
	}
	if plan.MinCheckInterval > demoCheckEvery { // the demo checks every 30 s to show changes quickly
		plan.MinCheckInterval = demoCheckEvery
		if err := plans.Set(ctx, tid, plan); err != nil {
			return err
		}
	}
	admin := uptime.NewAdmin(uptime.AdminDeps{Monitors: st, States: st, History: st, Plans: st, Sched: noSchedule{},
		PingAvailable: probe.PingAvailable, Now: time.Now})
	existing, err := st.ListMonitors(ctx, sc)
	if err != nil {
		return err
	}
	byName := map[string]domain.Monitor{}
	for _, m := range existing {
		byName[m.Name] = m
	}
	var errs []error
	for _, d := range targets {
		m := domain.Monitor{Name: d.name, Kind: d.kind, Target: d.target, Interval: demoCheckEvery, Timeout: 5 * time.Second,
			ConfirmRetries: 1}
		if d.kind == domain.MonitorHTTP {
			m.Method = "GET"
			if d.keyword != "" {
				m.Keyword, m.KeywordMode = d.keyword, domain.KeywordPresent
			}
		}
		if cur, ok := byName[d.name]; ok {
			if cur.Target == d.target {
				continue
			}
			m.ID, m.Channels = cur.ID, cur.Channels
			_, err = admin.Update(ctx, sc, m)
		} else {
			_, err = admin.Create(ctx, sc, m)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("demo monitor %s: %w", d.name, err))
		}
	}
	slog.Info("demo monitors ready", "monitors", len(targets))
	return errors.Join(errs...)
}

// noSchedule: the scheduler re-reads monitors on its own every few seconds.
type noSchedule struct{}

func (noSchedule) Refresh()            {}
func (noSchedule) CheckNow(int64) bool { return false }

// demoJitter is simulated latency: a shape for charts, never a secret.
func demoJitter(n int) int { return rand.IntN(n) } //nolint:gosec // simulated latency
