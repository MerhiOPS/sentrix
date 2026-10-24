package httpapi

import (
	"net/http"
	"net/url"
	"strings"
)

// sameOrigin reports whether the request's Origin is this server. Browsers
// send Origin on every cross-site POST and WebSocket upgrade, so requiring it
// to match closes CSRF even if SameSite were bypassed. A request without
// Origin on a state-changing method is refused: every browser that matters
// sends it, and non-browser clients have no session cookie anyway.
func sameOrigin(r *http.Request, extra []string) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return false
	}
	for _, allowed := range extra {
		if strings.EqualFold(o, allowed) {
			return true
		}
	}
	u, err := url.Parse(o)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// requireOrigin wraps a state-changing handler.
func (a *API) requireOrigin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r, a.cfg.ExtraOrigins) {
			writeJSON(w, http.StatusForbidden, errorBody{Error: "cross-origin request refused"})
			return
		}
		next(w, r)
	}
}
