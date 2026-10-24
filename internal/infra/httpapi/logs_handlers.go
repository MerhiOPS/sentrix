package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

func (a *API) handleSearchLogs(w http.ResponseWriter, r *http.Request) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	req, err := parseLogSearch(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	page, err := a.cfg.Logs.Search(r.Context(), scope, req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toLogPage(page))
}

func parseLogSearch(r *http.Request) (query.LogSearchRequest, error) {
	q := r.URL.Query()
	var req query.LogSearchRequest
	var err error
	if req.From, err = time.Parse(time.RFC3339, q.Get("from")); err != nil {
		return req, fmt.Errorf("%w: from must be RFC 3339", domain.ErrInvalid)
	}
	if req.To, err = time.Parse(time.RFC3339, q.Get("to")); err != nil {
		return req, fmt.Errorf("%w: to must be RFC 3339", domain.ErrInvalid)
	}
	if req.Hosts, err = parseHostList(q.Get("hosts")); err != nil {
		return req, err
	}
	req.Levels = parseLevels(q.Get("levels"))
	req.Q, req.Cursor = q.Get("q"), q.Get("cursor")
	if s := q.Get("limit"); s != "" {
		if req.Limit, err = strconv.Atoi(s); err != nil || req.Limit < 1 || req.Limit > query.MaxLogLimit {
			return req, fmt.Errorf("%w: limit must be 1..%d", domain.ErrInvalid, query.MaxLogLimit)
		}
	}
	return req, nil
}

// parseHostList parses comma-separated host ids. A malformed id is a 400:
// it cannot be anybody's host.
func parseHostList(s string) ([]domain.HostID, error) {
	var out []domain.HostID
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		id, err := domain.ParseHostID(p)
		if err != nil {
			return nil, fmt.Errorf("%w: host id %q", domain.ErrInvalid, p)
		}
		out = append(out, id)
	}
	return out, nil
}

func parseLevels(s string) []domain.Level {
	var out []domain.Level
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, domain.ParseLevel(p))
		}
	}
	return out
}

func (a *API) handleListRules(w http.ResponseWriter, r *http.Request) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	rules, err := a.cfg.Rules.List(r.Context(), scope)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := ruleListDTO{Rules: make([]ruleDTO, 0, len(rules))}
	for _, rr := range rules {
		out.Rules = append(out.Rules, toRule(rr))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in ruleDTO
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&in); err != nil {
		writeError(w, r, fmt.Errorf("%w: malformed rule", domain.ErrInvalid))
		return
	}
	rule, err := a.cfg.Rules.Create(r.Context(), scope, domain.RedactionRule{
		Pattern: in.Pattern, Replacement: in.Replacement, Action: domain.RedactionAction(in.Action)})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toRule(rule))
}

func (a *API) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("rid"), 10, 64)
	if err != nil {
		writeError(w, r, fmt.Errorf("rule: %w", domain.ErrNotFound))
		return
	}
	if err := a.cfg.Rules.Delete(r.Context(), scope, id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
