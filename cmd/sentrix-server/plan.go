package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// tenantPlan: sentrix-server tenant plan --data DIR --tenant ID [limits…]
// Flags not given keep their current value; with none it prints the plan
// and this month's usage.
func tenantPlan(args []string) error {
	fs := flag.NewFlagSet("tenant plan", flag.ContinueOnError)
	data := fs.String("data", "", "data directory")
	tenant := fs.String("tenant", "", "tenant id")
	maxHosts := fs.Int("max-hosts", 0, "enabled hosts allowed (new enrollments refused beyond it)")
	logMB := fs.Int64("log-mb-per-day", 0, "stored log megabytes per day (then logs are shed, metrics kept)")
	metricsDays := fs.Int("metrics-days", 0, "how long any metric history is kept")
	logsDays := fs.Int("logs-days", 0, "how long logs are kept")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tid, err := domain.ParseTenantID(*tenant)
	if err != nil {
		return fmt.Errorf("--tenant: %w", err)
	}
	scope, err := domain.NewScope(tid)
	if err != nil {
		return err
	}
	ctx := context.Background()
	st, closeDB, err := openStore(ctx, *data)
	if err != nil {
		return err
	}
	plans := identity.NewPlans(st, time.Now)
	p, u, err := plans.Get(ctx, scope)
	if err != nil {
		return errors.Join(err, closeDB())
	}
	changed := false
	fs.Visit(func(f *flag.Flag) {
		changed = changed || f.Name != "data" && f.Name != "tenant"
		switch f.Name {
		case "max-hosts":
			p.MaxHosts = *maxHosts
		case "log-mb-per-day":
			p.LogBytesPerDay = *logMB << 20
		case "metrics-days":
			p.MetricsRetentionDays = *metricsDays
		case "logs-days":
			p.LogsRetentionDays = *logsDays
		}
	})
	if changed {
		if err := plans.Set(ctx, tid, p); err != nil {
			return errors.Join(err, closeDB())
		}
		fmt.Println("plan updated")
	}
	fmt.Printf("hosts %d / %d · logs today %d / %d MB · metrics kept %d d · logs kept %d d\n",
		u.Hosts, p.MaxHosts, u.LogBytesToday>>20, p.LogBytesPerDay>>20, p.MetricsRetentionDays, p.LogsRetentionDays)
	fmt.Printf("this month: %d samples, %d MB of logs, peak %d hosts on one day\n", u.MonthSamples, u.MonthLogBytes>>20, u.MonthPeakHosts)
	return closeDB()
}
