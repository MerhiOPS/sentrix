package httpapi

import (
	"fmt"
	"net/http"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

func (a *API) handleListHosts(w http.ResponseWriter, r *http.Request) {
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
	latest, err := a.cfg.Hosts.LatestCore(r.Context(), scope)
	if err != nil {
		writeError(w, r, err)
		return
	}
	now := a.cfg.Now()
	out := hostListDTO{Hosts: make([]hostDTO, 0, len(hosts)), ServerTime: now.UTC()}
	for _, h := range hosts {
		var l *domain.CoreSample
		if s, ok := latest[h.ID]; ok {
			l = &s
		}
		out.Hosts = append(out.Hosts, toHost(h, l, now))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleGetHost(w http.ResponseWriter, r *http.Request) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	id, err := pathHost(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	h, err := a.cfg.Hosts.GetHost(r.Context(), scope, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	latest, err := a.cfg.Hosts.LatestCore(r.Context(), scope)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var l *domain.CoreSample
	if s, ok := latest[id]; ok {
		l = &s
	}
	writeJSON(w, http.StatusOK, toHost(h, l, a.cfg.Now()))
}

// pathHost parses {id}. A malformed id is 404, like an unknown one: the
// response must not tell "no such host" from "not a host id at all".
func pathHost(r *http.Request) (domain.HostID, error) {
	id, err := domain.ParseHostID(r.PathValue("id"))
	if err != nil {
		return domain.HostID{}, fmt.Errorf("host: %w", domain.ErrNotFound)
	}
	return id, nil
}
