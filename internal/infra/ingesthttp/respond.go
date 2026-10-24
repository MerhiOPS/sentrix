package ingesthttp

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/wire"
)

// statusFor maps an error onto the response contract the agent relies on:
//
//	400/413/422  permanently invalid — drop and count, never retry
//	401/403      credential unknown or host disabled — stop shipping
//	5xx          nothing stored — back off and resend the same batch
//
// Anything unrecognised is 500, the retryable direction: wrongly retrying is
// recoverable, wrongly dropping is data loss.
func statusFor(err error) int {
	switch {
	case errors.Is(err, errTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, errMalformed):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrInvalid):
		return http.StatusUnprocessableEntity
	case errors.Is(err, domain.ErrUnauthenticated):
		return http.StatusUnauthorized
	case errors.Is(err, domain.ErrForbidden), errors.Is(err, domain.ErrQuotaExceeded):
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}

// clientMessage hides server-side detail on 5xx: the agent only needs to
// know to retry, and internals do not belong on an internet-facing port.
func clientMessage(status int, err error) string {
	if status >= 500 {
		return "internal error"
	}
	return err.Error()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("ingest: write response", "err", err)
	}
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status := statusFor(err)
	if status >= 500 {
		slog.Error("ingest: request failed", "path", r.URL.Path, "err", err)
	}
	writeJSON(w, status, wire.Error{Error: clientMessage(status, err)})
}
