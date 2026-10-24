package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// ServerStats is the process-wide half of the system report.
type ServerStats struct {
	DBBytes        int64      `json:"db_bytes"`
	DBWALBytes     int64      `json:"db_wal_bytes"`
	DiskFreeRatio  *float64   `json:"disk_free_ratio"`
	TTLOverdueS    int64      `json:"ttl_overdue_s"`
	JanitorLastRun *time.Time `json:"janitor_last_run"`
	WSSubscribers  int        `json:"ws_subscribers"`
	WSDropped      uint64     `json:"ws_dropped"`
	WriterWaits    int64      `json:"writer_waits"`
	WriterWaitMS   int64      `json:"writer_wait_ms"`
}

// fleetStats is the tenant half: computed from the tenant's own hosts.
type fleetStats struct {
	Hosts         int    `json:"hosts"`
	Reporting     int    `json:"reporting"`
	Stale         int    `json:"stale"`
	NoData        int    `json:"no_data"`
	Disabled      int    `json:"disabled"`
	MaxHostLagS   int64  `json:"max_host_lag_s"`
	AgentWALMax   int    `json:"agent_wal_depth_max"`
	AgentWALTotal int    `json:"agent_wal_depth_total"`
	AgentDropped  uint64 `json:"agent_dropped_total"`
}

type systemDTO struct {
	ServerTime time.Time   `json:"server_time"`
	Fleet      fleetStats  `json:"fleet"`
	Server     ServerStats `json:"server"`
}

// readyTimeout bounds the readiness probe: a writer held longer than this
// is "not draining".
const readyTimeout = 2 * time.Second

// handleHealthz: the process is alive and serving. No dependency checks —
// a liveness probe that fails on a busy database restarts a healthy server.
func (a *API) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReadyz: the database answers a read and the writer is draining.
func (a *API) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
	defer cancel()
	if a.cfg.Ready != nil {
		if err := a.cfg.Ready(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not ready"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (a *API) handleSystem(w http.ResponseWriter, r *http.Request) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	hosts, err := a.cfg.Hosts.ListHosts(r.Context(), scope)
	if err != nil {
		writeError(w, r, err)
		return
	}
	now := a.cfg.Now()
	out := systemDTO{ServerTime: now, Fleet: fleetOf(hosts, now)}
	if a.cfg.System != nil {
		if out.Server, err = a.cfg.System(r.Context()); err != nil {
			writeError(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func fleetOf(hosts []domain.Host, now time.Time) fleetStats {
	var f fleetStats
	for _, h := range hosts {
		f.Hosts++
		switch h.Status(now) {
		case domain.StatusOK:
			f.Reporting++
		case domain.StatusStale:
			f.Stale++
		case domain.StatusNoData:
			f.NoData++
		case domain.StatusDisabled:
			f.Disabled++
			continue
		}
		if !h.LastSeen.IsZero() {
			f.MaxHostLagS = max(f.MaxHostLagS, int64(now.Sub(h.LastSeen)/time.Second))
		}
		f.AgentWALMax = max(f.AgentWALMax, h.Health.BufferDepth)
		f.AgentWALTotal += h.Health.BufferDepth
		f.AgentDropped += h.Health.DroppedTotal
	}
	return f
}
