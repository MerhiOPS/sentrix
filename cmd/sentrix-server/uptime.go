package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/alerting"
	"github.com/MerhiOPS/sentrix/internal/app/uptime"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/netguard"
	"github.com/MerhiOPS/sentrix/internal/infra/notify"
	"github.com/MerhiOPS/sentrix/internal/infra/probe"
	"github.com/MerhiOPS/sentrix/internal/infra/sqlite"
)

// uptimeOptions are the operator's uptime settings (serve flags).
type uptimeOptions struct {
	workers      int
	allowPrivate bool
	references   []string
}

// startUptime runs the uptime scheduler and its own notification
// dispatcher until ctx ends; a slow alert webhook cannot delay a down
// notification, nor the reverse.
func startUptime(ctx context.Context, st *sqlite.Store, o uptimeOptions) *uptime.Scheduler {
	log := slog.Default().With("component", "uptime")
	policy := netguard.Policy{AllowPrivate: o.allowPrivate}
	if o.allowPrivate {
		log.Warn("uptime checks may reach private addresses (--uptime-allow-private); cloud metadata stays blocked")
	}
	if err := probe.PingAvailable(); err != nil {
		log.Warn("ping monitors will be refused", "reason", err)
	}
	// References are the operator's own choice, so they may be private.
	refs := uptime.NewReferences(probe.New(netguard.Policy{AllowPrivate: true}), o.references, time.Now)
	if !refs.Configured() {
		log.Info("no --uptime-reference set: if this server loses its network, every monitor will report down")
	}
	out := alerting.NewDispatcher(notify.New(), st, log)
	checker := uptime.NewChecker(uptime.CheckerDeps{Probe: probe.New(policy), States: st, Channels: st, Silences: st, Out: out, Refs: refs})
	cfg := uptime.DefaultSchedulerConfig
	cfg.Workers, cfg.PerTenant = o.workers, max(1, o.workers/4)
	sched := uptime.NewScheduler(st, checker.Check, cfg, time.Now, log)
	if ops, err := domain.NewOpsScope("startup sweep of undelivered monitor events"); err == nil {
		if n, err := st.FailUndeliveredMonitorEvents(ctx, ops, uptime.UndeliveredReason); err != nil {
			log.Error("sweep undelivered monitor events", "err", err)
		} else if n > 0 {
			log.Warn("monitor notifications left undelivered by the previous run marked failed", "events", n)
		}
	}
	go out.Run(ctx)
	go sched.Run(ctx)
	return sched
}
