package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

func (a *API) handleMetrics(w http.ResponseWriter, r *http.Request) {
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
	req, err := parseSeries(r, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	f, err := a.cfg.Series.Series(r.Context(), scope, req)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toFrame(f))
}

func parseSeries(r *http.Request, host domain.HostID) (query.SeriesRequest, error) {
	q := r.URL.Query()
	req := query.SeriesRequest{Host: host}
	for _, g := range strings.Split(q.Get("gauges"), ",") {
		if g = strings.TrimSpace(g); g != "" {
			req.Gauges = append(req.Gauges, query.Gauge(g))
		}
	}
	var err error
	if req.From, err = time.Parse(time.RFC3339, q.Get("from")); err != nil {
		return req, fmt.Errorf("%w: from must be RFC 3339", domain.ErrInvalid)
	}
	if req.To, err = time.Parse(time.RFC3339, q.Get("to")); err != nil {
		return req, fmt.Errorf("%w: to must be RFC 3339", domain.ErrInvalid)
	}
	if s := q.Get("step"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 86400*31 {
			return req, fmt.Errorf("%w: step must be 1..2678400 seconds", domain.ErrInvalid)
		}
		req.Step = time.Duration(n) * time.Second
	}
	return req, nil
}
