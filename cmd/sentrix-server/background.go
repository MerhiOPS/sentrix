package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/retention"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/diskguard"
	"github.com/MerhiOPS/sentrix/internal/infra/httpapi"
	"github.com/MerhiOPS/sentrix/internal/infra/livehub"
	"github.com/MerhiOPS/sentrix/internal/infra/sqlite"
)

// janitorEvery is how often retention runs. Each run deletes in bounded
// batches, so a long backlog (first run after an upgrade) spreads out.
const janitorEvery = 10 * time.Minute

// startJanitor runs retention once at start and then periodically.
func startJanitor(ctx context.Context, st *sqlite.Store) *retention.Janitor {
	j := retention.New(st, domain.DefaultRetention, time.Now)
	log := slog.Default().With("component", "retention")
	run := func() {
		n, err := j.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			log.Error("retention run", "err", err)
		}
		if overdue, _ := j.Overdue(); n > 0 || overdue > 0 {
			log.Info("retention run", "deleted", n, "ttl_overdue", overdue.Round(time.Second))
		}
	}
	go func() {
		run()
		t := time.NewTicker(janitorEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				run()
			}
		}
	}()
	return j
}

// systemReport builds the server half of /api/v1/system.
func systemReport(dataDir string, st *sqlite.Store, j *retention.Janitor, disk *diskguard.Guard, hub *livehub.Hub) func(context.Context) (httpapi.ServerStats, error) {
	return func(context.Context) (httpapi.ServerStats, error) {
		var s httpapi.ServerStats
		s.DBBytes = fileSize(filepath.Join(dataDir, dbFile))
		s.DBWALBytes = fileSize(filepath.Join(dataDir, dbFile+"-wal"))
		if r, err := disk.FreeRatio(); err == nil {
			s.DiskFreeRatio = &r
		}
		overdue, last := j.Overdue()
		s.TTLOverdueS = int64(overdue / time.Second)
		if !last.IsZero() {
			s.JanitorLastRun = &last
		}
		s.WSSubscribers, s.WSDropped = hub.Subscribers(), hub.SlowDropped()
		s.WriterWaits, s.WriterWaitMS = st.WriterWaits()
		return s, nil
	}
}

func fileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}
