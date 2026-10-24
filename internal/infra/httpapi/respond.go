// Package httpapi is the dashboard's REST + WebSocket API and the embedded
// web app. Its responses follow docs/openapi.yaml, which tests enforce.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

type errorBody struct {
	Error string `json:"error"`
}

// statusFor maps domain errors to HTTP. Not-found covers another tenant's
// resources too: existence is never leaked across tenants (404, not 403).
func statusFor(err error) int {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrUnauthenticated):
		return http.StatusUnauthorized
	case errors.Is(err, domain.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, domain.ErrQuotaExceeded):
		return http.StatusTooManyRequests
	case errors.Is(err, domain.ErrConflict):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("api: write response", "err", err)
	}
}

// writeError sends the error. 5xx bodies carry no internals; 404 bodies are
// uniform so they cannot distinguish "absent" from "another tenant's".
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status := statusFor(err)
	msg := err.Error()
	switch {
	case status >= 500:
		slog.Error("api: request failed", "path", r.URL.Path, "err", err)
		msg = "internal error"
	case status == http.StatusNotFound:
		msg = "not found"
	}
	writeJSON(w, status, errorBody{Error: msg})
}
