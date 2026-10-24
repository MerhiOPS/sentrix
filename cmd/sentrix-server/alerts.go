package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/alerting"
	"github.com/MerhiOPS/sentrix/internal/infra/notify"
	"github.com/MerhiOPS/sentrix/internal/infra/sqlite"
)

// evalPoll is how often the evaluator checks for a newly closed bucket.
// Tick is a no-op until one closes, so this only bounds the extra delay.
const evalPoll = 10 * time.Second

// startAlerting runs the evaluator and the notification dispatcher until ctx
// ends. Both are off the ingest path.
func startAlerting(ctx context.Context, st *sqlite.Store) {
	log := slog.Default().With("component", "alerting")
	out := alerting.NewDispatcher(notify.New(), st, log)
	eval := alerting.NewEvaluator(alerting.Deps{
		Tenants: st, Hosts: st, Rules: st, Channels: st, Silences: st, States: st, Buckets: st,
	}, out, time.Now)
	go out.Run(ctx)
	go func() {
		t := time.NewTicker(evalPoll)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := eval.Tick(ctx); err != nil {
					log.Error("alert evaluation", "err", err)
				}
			}
		}
	}()
}
