package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

const maxAlertBody = 64 << 10

// decodeBody reads a bounded JSON body; unknown fields are refused so a
// misspelt field is an error, not a silently ignored setting.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAlertBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: malformed body", domain.ErrInvalid)
	}
	return nil
}

// pathInt parses an integer path id; a malformed one is not found.
func pathInt(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, domain.ErrNotFound)
	}
	return id, nil
}

func (a *API) handleListAlertRules(w http.ResponseWriter, r *http.Request) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	rules, err := a.cfg.Alerts.Rules(r.Context(), scope)
	if err != nil {
		writeError(w, r, err)
		return
	}
	hosts, err := a.cfg.Hosts.ListHosts(r.Context(), scope)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := alertRuleListDTO{Rules: make([]alertRuleDTO, 0, len(rules))}
	for _, rule := range rules {
		out.Rules = append(out.Rules, toAlertRule(rule, inert(rule, hosts)))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleCreateAlertRule(w http.ResponseWriter, r *http.Request) {
	scope, rule, ok := a.ruleFromBody(w, r)
	if !ok {
		return
	}
	created, err := a.cfg.Alerts.CreateRule(r.Context(), scope, rule)
	if err != nil {
		writeError(w, r, err)
		return
	}
	hosts, _ := a.cfg.Hosts.ListHosts(r.Context(), scope)
	writeJSON(w, http.StatusCreated, toAlertRule(created, inert(created, hosts)))
}

func (a *API) handleUpdateAlertRule(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "rid")
	if err != nil {
		writeError(w, r, err)
		return
	}
	scope, rule, ok := a.ruleFromBody(w, r)
	if !ok {
		return
	}
	rule.ID = id
	if err := a.cfg.Alerts.UpdateRule(r.Context(), scope, rule); err != nil {
		writeError(w, r, err)
		return
	}
	hosts, _ := a.cfg.Hosts.ListHosts(r.Context(), scope)
	writeJSON(w, http.StatusOK, toAlertRule(rule, inert(rule, hosts)))
}

func (a *API) ruleFromBody(w http.ResponseWriter, r *http.Request) (domain.Scope, domain.AlertRule, bool) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return scope, domain.AlertRule{}, false
	}
	var in alertRuleDTO
	if err := decodeBody(w, r, &in); err != nil {
		writeError(w, r, err)
		return scope, domain.AlertRule{}, false
	}
	rule, err := in.toDomain()
	if err != nil {
		writeError(w, r, err)
		return scope, domain.AlertRule{}, false
	}
	return scope, rule, true
}

func (a *API) handleDeleteAlertRule(w http.ResponseWriter, r *http.Request) {
	a.deleteByID(w, r, "rid", a.cfg.Alerts.DeleteRule)
}

func (a *API) handleAlerts(w http.ResponseWriter, r *http.Request) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	states, err := a.cfg.Alerts.States(r.Context(), scope)
	if err != nil {
		writeError(w, r, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := a.cfg.Alerts.Events(r.Context(), scope, limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAlerts(states, events))
}

func (a *API) handleHostDisable(w http.ResponseWriter, r *http.Request) {
	a.hostLifecycle(w, r, a.cfg.HostAdmin.Disable)
}

func (a *API) handleHostEnable(w http.ResponseWriter, r *http.Request) {
	a.hostLifecycle(w, r, a.cfg.HostAdmin.Enable)
}

func (a *API) hostLifecycle(w http.ResponseWriter, r *http.Request, act func(context.Context, domain.Scope, domain.HostID) error) {
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
	if err := act(r.Context(), scope, id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteByID runs a scoped delete on an integer path id.
func (a *API) deleteByID(w http.ResponseWriter, r *http.Request, name string, del func(context.Context, domain.Scope, int64) error) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	id, err := pathInt(r, name)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := del(r.Context(), scope, id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
