package ingesthttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/wire"
)

// Authenticator resolves an agent credential.
type Authenticator interface {
	Authenticate(ctx context.Context, credential []byte) (ingest.AuthHost, error)
}

// Ingester stores a batch.
type Ingester interface {
	Ingest(ctx context.Context, ah ingest.AuthHost, b ingest.Batch) (ingest.Result, error)
}

// Enroller redeems enrollment tokens.
type Enroller interface {
	Enroll(ctx context.Context, tokenSecret []byte, info identity.HostInfo) (domain.Host, []byte, error)
}

// Handler serves /ingest/v1.
type Handler struct {
	auth   Authenticator
	ingest Ingester
	enroll Enroller
	mux    *http.ServeMux
}

// NewHandler wires the endpoints.
func NewHandler(auth Authenticator, ing Ingester, enr Enroller) *Handler {
	h := &Handler{auth: auth, ingest: ing, enroll: enr, mux: http.NewServeMux()}
	h.mux.HandleFunc("POST "+wire.PathBatch, h.handleBatch)
	h.mux.HandleFunc("POST "+wire.PathEnroll, h.handleEnroll)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

func (h *Handler) handleBatch(w http.ResponseWriter, r *http.Request) {
	cred, err := bearer(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	ah, err := h.auth.Authenticate(r.Context(), cred)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var b wire.Batch
	if err := decodeBody(w, r, wire.MaxCompressedBytes, wire.MaxDecompressedBytes, &b); err != nil {
		writeError(w, r, err)
		return
	}
	if len(b.Samples) > wire.MaxSamplesPerBatch || len(b.Details) > wire.MaxDetailsPerBatch ||
		len(b.Processes) > wire.MaxSnapshotsPerBatch {
		writeError(w, r, fmt.Errorf("%w: batch over per-batch limits", errTooLarge))
		return
	}
	res, err := h.ingest.Ingest(r.Context(), ah, toBatch(b))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAck(b.BatchSeq, res))
}

func (h *Handler) handleEnroll(w http.ResponseWriter, r *http.Request) {
	var req wire.EnrollRequest
	if err := decodeBody(w, r, wire.MaxEnrollBytes, wire.MaxEnrollBytes, &req); err != nil {
		writeError(w, r, err)
		return
	}
	tok, err := wire.ParseToken(req.Token)
	if err != nil {
		writeError(w, r, fmt.Errorf("%w: %w", errMalformed, err))
		return
	}
	host, cred, err := h.enroll.Enroll(r.Context(), tok.Secret, toHostInfo(req.Host))
	if err != nil {
		writeError(w, r, enrollError(err))
		return
	}
	writeJSON(w, http.StatusOK, wire.EnrollResponse{Credential: wire.EncodeSecret(cred), HostID: host.ID.String()})
}

// enrollError: an unknown/used/expired token is a credential failure (401),
// not a missing resource.
func enrollError(err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("%w: enrollment token invalid, expired or already used", domain.ErrUnauthenticated)
	}
	return err
}

// bearer extracts and decodes "Authorization: Bearer <credential>".
func bearer(r *http.Request) ([]byte, error) {
	scheme, value, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, wire.AuthScheme) {
		return nil, fmt.Errorf("%w: missing bearer credential", domain.ErrUnauthenticated)
	}
	cred, err := wire.DecodeSecret(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrUnauthenticated, err)
	}
	return cred, nil
}
