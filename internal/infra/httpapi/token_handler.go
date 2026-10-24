package httpapi

import "net/http"

// handleCreateToken issues an enrollment token (admin only, enforced by the
// route). The secret is shown once and never stored.
func (a *API) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	secret, expires, err := a.cfg.Tokens.IssueToken(r.Context(), scope)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, tokenDTO{Token: a.cfg.FormatToken(secret), ExpiresAt: expires})
}
