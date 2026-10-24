package httpapi

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// maxLoginBody bounds the login request (the password limit is far below).
const maxLoginBody = 4 << 10

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxLoginBody)).Decode(&req); err != nil {
		writeError(w, r, fmt.Errorf("%w: malformed login request", domain.ErrInvalid))
		return
	}
	token, sess, err := a.cfg.Sessions.Login(r.Context(), req.Email, req.Password, clientAddr(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	scope, err := domain.NewScope(sess.Tenant)
	if err != nil {
		writeError(w, r, err)
		return
	}
	u, err := a.cfg.Users.GetUser(r.Context(), scope, sess.User)
	if err != nil {
		writeError(w, r, err)
		return
	}
	http.SetCookie(w, sessionCookie(token))
	writeJSON(w, http.StatusOK, toMe(u))
}

func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := a.cfg.Sessions.Logout(r.Context(), sessionFrom(r.Context())); err != nil {
		writeError(w, r, err)
		return
	}
	http.SetCookie(w, clearedCookie())
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r.Context())
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	u, err := a.cfg.Users.GetUser(r.Context(), scope, sess.User)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toMe(u))
}

// clientAddr is the peer address used to key login throttling. Forwarded
// headers are not trusted: behind a proxy every login would share one key,
// which throttles harder, never softer.
func clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
