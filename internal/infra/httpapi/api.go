package httpapi

import (
	"context"
	"io/fs"
	"net/http"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/livehub"
)

// SessionService is the identity use case the API needs.
type SessionService interface {
	Login(ctx context.Context, email, password, client string) ([]byte, domain.Session, error)
	Authenticate(ctx context.Context, token []byte) (domain.Session, error)
	Logout(ctx context.Context, sess domain.Session) error
}

// Users reads the current user (for email and role display).
type Users interface {
	GetUser(ctx context.Context, s domain.Scope, id domain.UserID) (domain.User, error)
}

// TokenIssuer issues enrollment tokens.
type TokenIssuer interface {
	IssueToken(ctx context.Context, s domain.Scope) ([]byte, time.Time, error)
}

// Hosts is the host read side.
type Hosts interface {
	query.HostReader
	query.LatestReader
}

// Series answers chart queries.
type Series interface {
	Series(ctx context.Context, s domain.Scope, req query.SeriesRequest) (query.Frame, error)
}

// DetailSeries answers per-core / per-mount / per-device / per-NIC charts.
type DetailSeries interface {
	Series(ctx context.Context, s domain.Scope, host domain.HostID) ([]domain.Series, error)
	Frame(ctx context.Context, s domain.Scope, req query.DetailRequest) (query.DetailFrame, error)
}

// Live is the in-process feed.
type Live interface {
	Subscribe(tenant domain.TenantID, hosts []domain.HostID, since time.Time) *livehub.Sub
	Unsubscribe(sub *livehub.Sub)
}

// Config is everything the API depends on.
type Config struct {
	Sessions SessionService
	Users    Users
	Tokens   TokenIssuer
	Hosts    Hosts
	Series   Series
	Details  DetailSeries
	Procs    query.ProcessReader
	Live     Live
	// FormatToken renders an issued secret for the operator (adds the
	// server's SPKI pin in self-signed mode).
	FormatToken  func(secret []byte) string
	Web          fs.FS    // built dashboard; nil serves the API only
	ExtraOrigins []string // e.g. the Vite dev server
	Now          func() time.Time
}

// API is the dashboard HTTP handler.
type API struct {
	cfg Config
	mux *http.ServeMux
}

// New builds the API and registers every route.
func New(cfg Config) *API {
	a := &API{cfg: cfg, mux: http.NewServeMux()}
	viewer, admin := domain.RoleViewer, domain.RoleAdmin
	a.mux.HandleFunc("POST /api/v1/auth/login", a.requireOrigin(a.handleLogin))
	a.mux.HandleFunc("POST /api/v1/auth/logout", a.requireOrigin(a.requireSession(viewer, a.handleLogout)))
	a.mux.HandleFunc("GET /api/v1/me", a.requireSession(viewer, a.handleMe))
	a.mux.HandleFunc("GET /api/v1/hosts", a.requireSession(viewer, a.handleListHosts))
	a.mux.HandleFunc("GET /api/v1/hosts/{id}", a.requireSession(viewer, a.handleGetHost))
	a.mux.HandleFunc("GET /api/v1/hosts/{id}/metrics", a.requireSession(viewer, a.handleMetrics))
	a.mux.HandleFunc("GET /api/v1/hosts/{id}/series", a.requireSession(viewer, a.handleListSeries))
	a.mux.HandleFunc("GET /api/v1/hosts/{id}/series/{sid}/metrics", a.requireSession(viewer, a.handleSeriesMetrics))
	a.mux.HandleFunc("GET /api/v1/hosts/{id}/processes", a.requireSession(viewer, a.handleProcesses))
	a.mux.HandleFunc("POST /api/v1/enrollment-tokens", a.requireOrigin(a.requireSession(admin, a.handleCreateToken)))
	a.mux.HandleFunc("GET /api/v1/stream", a.requireSession(viewer, a.handleStream))
	a.mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, errorBody{Error: "not found"})
	})
	if cfg.Web != nil {
		a.mux.Handle("/", spa(cfg.Web))
	}
	return a
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'")
	a.mux.ServeHTTP(w, r)
}
