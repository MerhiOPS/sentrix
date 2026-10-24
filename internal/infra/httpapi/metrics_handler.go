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
	rg, err := parseRange(r)
	if err != nil {
		return query.SeriesRequest{}, err
	}
	req := query.SeriesRequest{Host: host, From: rg.from, To: rg.to, Step: rg.step}
	for _, g := range strings.Split(r.URL.Query().Get("gauges"), ",") {
		if g = strings.TrimSpace(g); g != "" {
			req.Gauges = append(req.Gauges, query.Gauge(g))
		}
	}
	return req, nil
}

type timeRange struct {
	from, to time.Time
	step     time.Duration
}

// parseRange reads from, to (RFC 3339) and the optional step in seconds.
func parseRange(r *http.Request) (timeRange, error) {
	q := r.URL.Query()
	var rg timeRange
	var err error
	if rg.from, err = time.Parse(time.RFC3339, q.Get("from")); err != nil {
		return rg, fmt.Errorf("%w: from must be RFC 3339", domain.ErrInvalid)
	}
	if rg.to, err = time.Parse(time.RFC3339, q.Get("to")); err != nil {
		return rg, fmt.Errorf("%w: to must be RFC 3339", domain.ErrInvalid)
	}
	if s := q.Get("step"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 86400*31 {
			return rg, fmt.Errorf("%w: step must be 1..2678400 seconds", domain.ErrInvalid)
		}
		rg.step = time.Duration(n) * time.Second
	}
	return rg, nil
}
