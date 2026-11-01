package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/uptime"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// MonitorAdmin manages uptime monitors and reads their history.
type MonitorAdmin interface {
	List(ctx context.Context, s domain.Scope) ([]uptime.View, error)
	Get(ctx context.Context, s domain.Scope, id int64) (uptime.View, error)
	Create(ctx context.Context, s domain.Scope, m domain.Monitor) (domain.Monitor, error)
	Update(ctx context.Context, s domain.Scope, m domain.Monitor) (domain.Monitor, error)
	Delete(ctx context.Context, s domain.Scope, id int64) error
	SetPaused(ctx context.Context, s domain.Scope, id int64, paused bool) error
	CheckNow(ctx context.Context, s domain.Scope, id int64) error
	Results(ctx context.Context, s domain.Scope, id int64, from, to time.Time) ([]domain.CheckResult, error)
	Incidents(ctx context.Context, s domain.Scope, id int64, limit int) ([]domain.Incident, []domain.MonitorEvent, error)
}

func (a *API) monitorRoutes(viewer, admin domain.Role) {
	write := func(h http.HandlerFunc) http.HandlerFunc { return a.requireOrigin(a.requireSession(admin, h)) }
	a.mux.HandleFunc("GET /api/v1/monitors", a.requireSession(viewer, a.handleListMonitors))
	a.mux.HandleFunc("POST /api/v1/monitors", write(a.handleCreateMonitor))
	a.mux.HandleFunc("GET /api/v1/monitors/{mid}", a.requireSession(viewer, a.handleGetMonitor))
	a.mux.HandleFunc("PUT /api/v1/monitors/{mid}", write(a.handleUpdateMonitor))
	a.mux.HandleFunc("DELETE /api/v1/monitors/{mid}", write(a.handleDeleteMonitor))
	a.mux.HandleFunc("POST /api/v1/monitors/{mid}/pause", write(a.pauseHandler(true)))
	a.mux.HandleFunc("POST /api/v1/monitors/{mid}/resume", write(a.pauseHandler(false)))
	a.mux.HandleFunc("POST /api/v1/monitors/{mid}/check", write(a.handleCheckNow))
	a.mux.HandleFunc("GET /api/v1/monitors/{mid}/results", a.requireSession(viewer, a.handleMonitorResults))
	a.mux.HandleFunc("GET /api/v1/monitors/{mid}/incidents", a.requireSession(viewer, a.handleMonitorIncidents))
}

// scopedMonitor resolves the session's scope and the {mid} path id.
func scopedMonitor(w http.ResponseWriter, r *http.Request) (domain.Scope, int64, bool) {
	scope, err := scopeFrom(r.Context())
	if err == nil {
		var id int64
		if id, err = pathInt(r, "mid"); err == nil {
			return scope, id, true
		}
	}
	writeError(w, r, err)
	return scope, 0, false
}

func (a *API) handleListMonitors(w http.ResponseWriter, r *http.Request) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	views, err := a.cfg.Monitors.List(r.Context(), scope)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := monitorListDTO{Monitors: make([]monitorDTO, 0, len(views)), PingAvailable: a.cfg.PingAvailable == nil || a.cfg.PingAvailable() == nil}
	for _, v := range views {
		out.Monitors = append(out.Monitors, toMonitor(v))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleGetMonitor(w http.ResponseWriter, r *http.Request) {
	scope, id, ok := scopedMonitor(w, r)
	if !ok {
		return
	}
	v, err := a.cfg.Monitors.Get(r.Context(), scope, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toMonitor(v))
}

func (a *API) handleCreateMonitor(w http.ResponseWriter, r *http.Request) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	m, ok := monitorFromBody(w, r)
	if !ok {
		return
	}
	created, err := a.cfg.Monitors.Create(r.Context(), scope, m)
	if err != nil {
		writeError(w, r, err)
		return
	}
	a.writeMonitor(w, r, scope, created.ID, http.StatusCreated)
}

func (a *API) handleUpdateMonitor(w http.ResponseWriter, r *http.Request) {
	scope, id, ok := scopedMonitor(w, r)
	if !ok {
		return
	}
	m, ok := monitorFromBody(w, r)
	if !ok {
		return
	}
	m.ID = id
	if _, err := a.cfg.Monitors.Update(r.Context(), scope, m); err != nil {
		writeError(w, r, err)
		return
	}
	a.writeMonitor(w, r, scope, id, http.StatusOK)
}

func (a *API) writeMonitor(w http.ResponseWriter, r *http.Request, scope domain.Scope, id int64, status int) {
	v, err := a.cfg.Monitors.Get(r.Context(), scope, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, status, toMonitor(v))
}

func monitorFromBody(w http.ResponseWriter, r *http.Request) (domain.Monitor, bool) {
	var in monitorInputDTO
	if err := decodeBody(w, r, &in); err != nil {
		writeError(w, r, err)
		return domain.Monitor{}, false
	}
	m, err := in.toDomain()
	if err != nil {
		writeError(w, r, err)
		return domain.Monitor{}, false
	}
	return m, true
}

func (a *API) handleDeleteMonitor(w http.ResponseWriter, r *http.Request) {
	a.deleteByID(w, r, "mid", a.cfg.Monitors.Delete)
}

func (a *API) pauseHandler(paused bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, id, ok := scopedMonitor(w, r)
		if !ok {
			return
		}
		if err := a.cfg.Monitors.SetPaused(r.Context(), scope, id, paused); err != nil {
			writeError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *API) handleCheckNow(w http.ResponseWriter, r *http.Request) {
	scope, id, ok := scopedMonitor(w, r)
	if !ok {
		return
	}
	if err := a.cfg.Monitors.CheckNow(r.Context(), scope, id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) handleMonitorResults(w http.ResponseWriter, r *http.Request) {
	scope, id, ok := scopedMonitor(w, r)
	if !ok {
		return
	}
	from, err1 := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	to, err2 := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
	if err1 != nil || err2 != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "from and to must be RFC 3339 times"})
		return
	}
	results, err := a.cfg.Monitors.Results(r.Context(), scope, id, from, to)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := struct {
		Results []checkResultDTO `json:"results"`
	}{Results: make([]checkResultDTO, 0, len(results))}
	for _, res := range results {
		out.Results = append(out.Results, toCheckResult(res))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleMonitorIncidents(w http.ResponseWriter, r *http.Request) {
	scope, id, ok := scopedMonitor(w, r)
	if !ok {
		return
	}
	limit := 50
	if q := r.URL.Query().Get("limit"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 1 || n > 500 {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: "limit must be 1-500"})
			return
		}
		limit = n
	}
	inc, evs, err := a.cfg.Monitors.Incidents(r.Context(), scope, id, limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toIncidents(inc, evs, a.cfg.Now()))
}
