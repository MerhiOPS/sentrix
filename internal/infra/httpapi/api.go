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
	SubscribeLogs(tenant domain.TenantID, f livehub.LogFilter) *livehub.LogSub
	UnsubscribeLogs(sub *livehub.LogSub)
}

// LogSearch answers log searches.
type LogSearch interface {
	Search(ctx context.Context, s domain.Scope, req query.LogSearchRequest) (query.LogPage, error)
}

// Rules manages a tenant's redaction rules.
type Rules interface {
	List(ctx context.Context, s domain.Scope) ([]domain.RedactionRule, error)
	Create(ctx context.Context, s domain.Scope, r domain.RedactionRule) (domain.RedactionRule, error)
	Delete(ctx context.Context, s domain.Scope, id int64) error
}

// AlertAdmin manages alert rules, channels and silences and reads alert
// state. Channel URLs it returns are already masked.
type AlertAdmin interface {
	Rules(ctx context.Context, s domain.Scope) ([]domain.AlertRule, error)
	CreateRule(ctx context.Context, s domain.Scope, r domain.AlertRule) (domain.AlertRule, error)
	UpdateRule(ctx context.Context, s domain.Scope, r domain.AlertRule) error
	DeleteRule(ctx context.Context, s domain.Scope, id int64) error
	Channels(ctx context.Context, s domain.Scope) ([]domain.Channel, error)
	CreateChannel(ctx context.Context, s domain.Scope, c domain.Channel) (domain.Channel, error)
	UpdateChannel(ctx context.Context, s domain.Scope, c domain.Channel) error
	DeleteChannel(ctx context.Context, s domain.Scope, id int64) error
	Silences(ctx context.Context, s domain.Scope) ([]domain.Silence, error)
	CreateSilence(ctx context.Context, s domain.Scope, sil domain.Silence) (domain.Silence, error)
	DeleteSilence(ctx context.Context, s domain.Scope, id int64) error
	States(ctx context.Context, s domain.Scope) ([]domain.RuleHostState, error)
	Events(ctx context.Context, s domain.Scope, limit int) ([]domain.AlertEvent, error)
}

// HostAdmin disables and re-enables hosts.
type HostAdmin interface {
	Disable(ctx context.Context, s domain.Scope, id domain.HostID) error
	Enable(ctx context.Context, s domain.Scope, id domain.HostID) error
}

// Config is everything the API depends on.
type Config struct {
	Sessions  SessionService
	Users     Users
	Tokens    TokenIssuer
	Hosts     Hosts
	Series    Series
	Details   DetailSeries
	Procs     query.ProcessReader
	Logs      LogSearch
	Rules     Rules
	Alerts    AlertAdmin
	HostAdmin HostAdmin
	// Ready probes the database for /readyz; System reports server-wide
	// figures for /api/v1/system. Either may be nil.
	Plans  PlanReader
	Ready  func(ctx context.Context) error
	System func(ctx context.Context) (ServerStats, error)
	Live   Live
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
	a.mux.HandleFunc("GET /api/v1/logs", a.requireSession(viewer, a.handleSearchLogs))
	a.mux.HandleFunc("GET /api/v1/redaction-rules", a.requireSession(admin, a.handleListRules))
	a.mux.HandleFunc("POST /api/v1/redaction-rules", a.requireOrigin(a.requireSession(admin, a.handleCreateRule)))
	a.mux.HandleFunc("DELETE /api/v1/redaction-rules/{rid}", a.requireOrigin(a.requireSession(admin, a.handleDeleteRule)))
	a.alertRoutes(viewer, admin)
	a.mux.HandleFunc("GET /healthz", a.handleHealthz)
	a.mux.HandleFunc("GET /readyz", a.handleReadyz)
	a.mux.HandleFunc("GET /api/v1/system", a.requireSession(admin, a.handleSystem))
	a.mux.HandleFunc("GET /api/v1/usage", a.requireSession(viewer, a.handleUsage))
	a.mux.HandleFunc("GET /api/v1/stream", a.requireSession(viewer, a.handleStream))
	a.mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, errorBody{Error: "not found"})
	})
	if cfg.Web != nil {
		a.mux.Handle("/", spa(cfg.Web))
	}
	return a
}

func (a *API) alertRoutes(viewer, admin domain.Role) {
	write := func(h http.HandlerFunc) http.HandlerFunc { return a.requireOrigin(a.requireSession(admin, h)) }
	a.mux.HandleFunc("POST /api/v1/hosts/{id}/disable", write(a.handleHostDisable))
	a.mux.HandleFunc("POST /api/v1/hosts/{id}/enable", write(a.handleHostEnable))
	a.mux.HandleFunc("GET /api/v1/alerts", a.requireSession(viewer, a.handleAlerts))
	a.mux.HandleFunc("GET /api/v1/alert-rules", a.requireSession(viewer, a.handleListAlertRules))
	a.mux.HandleFunc("POST /api/v1/alert-rules", write(a.handleCreateAlertRule))
	a.mux.HandleFunc("PUT /api/v1/alert-rules/{rid}", write(a.handleUpdateAlertRule))
	a.mux.HandleFunc("DELETE /api/v1/alert-rules/{rid}", write(a.handleDeleteAlertRule))
	a.mux.HandleFunc("GET /api/v1/channels", a.requireSession(viewer, a.handleListChannels))
	a.mux.HandleFunc("POST /api/v1/channels", write(a.handleCreateChannel))
	a.mux.HandleFunc("PUT /api/v1/channels/{cid}", write(a.handleUpdateChannel))
	a.mux.HandleFunc("DELETE /api/v1/channels/{cid}", write(a.handleDeleteChannel))
	a.mux.HandleFunc("GET /api/v1/silences", a.requireSession(viewer, a.handleListSilences))
	a.mux.HandleFunc("POST /api/v1/silences", write(a.handleCreateSilence))
	a.mux.HandleFunc("DELETE /api/v1/silences/{sid}", write(a.handleDeleteSilence))
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'")
	a.mux.ServeHTTP(w, r)
}
