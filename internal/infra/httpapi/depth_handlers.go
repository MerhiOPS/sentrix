package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

func (a *API) handleListSeries(w http.ResponseWriter, r *http.Request) {
	scope, host, ok := a.scopeAndHost(w, r)
	if !ok {
		return
	}
	ss, err := a.cfg.Details.Series(r.Context(), scope, host)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSeriesList(ss))
}

func (a *API) handleSeriesMetrics(w http.ResponseWriter, r *http.Request) {
	scope, host, ok := a.scopeAndHost(w, r)
	if !ok {
		return
	}
	sid, err := strconv.ParseInt(r.PathValue("sid"), 10, 64)
	if err != nil {
		writeError(w, r, fmt.Errorf("series: %w", domain.ErrNotFound))
		return
	}
	rg, err := parseRange(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	f, err := a.cfg.Details.Frame(r.Context(), scope, query.DetailRequest{
		Host: host, Series: sid, From: rg.from, To: rg.to, Step: rg.step})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toDetailFrame(f))
}

func (a *API) handleProcesses(w http.ResponseWriter, r *http.Request) {
	scope, host, ok := a.scopeAndHost(w, r)
	if !ok {
		return
	}
	at := a.cfg.Now()
	if v := r.URL.Query().Get("at"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, r, fmt.Errorf("%w: at must be RFC 3339", domain.ErrInvalid))
			return
		}
		at = t
	}
	snap, err := a.cfg.Procs.ProcessesAt(r.Context(), scope, host, at)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSnapshot(snap))
}

// scopeAndHost resolves the session scope and the {id} host, writing the
// error response itself when either fails.
func (a *API) scopeAndHost(w http.ResponseWriter, r *http.Request) (domain.Scope, domain.HostID, bool) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return domain.Scope{}, domain.HostID{}, false
	}
	host, err := pathHost(r)
	if err != nil {
		writeError(w, r, err)
		return domain.Scope{}, domain.HostID{}, false
	}
	return scope, host, true
}
