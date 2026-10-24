package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/alerting"
	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/app/redaction"
	"github.com/MerhiOPS/sentrix/internal/infra/diskguard"
	"github.com/MerhiOPS/sentrix/internal/infra/httpapi"
	"github.com/MerhiOPS/sentrix/internal/infra/ingesthttp"
	"github.com/MerhiOPS/sentrix/internal/infra/livehub"
	"github.com/MerhiOPS/sentrix/internal/infra/password"
	"github.com/MerhiOPS/sentrix/internal/infra/redact"
	"github.com/MerhiOPS/sentrix/internal/infra/sqlite"
	"github.com/MerhiOPS/sentrix/internal/wire"
	"github.com/MerhiOPS/sentrix/web"
)

// Composition constants.
const (
	authTTL         = 30 * time.Second // a disabled host keeps sending at most this long
	authCache       = 10_000
	hashParallel    = 2 // concurrent Argon2id operations: 2 × 46 MiB
	loginFailures   = 5
	loginWindow     = 15 * time.Minute
	loginTrackKeys  = 50_000
	shutdownTimeout = 15 * time.Second
	ruleCacheTTL    = 30 * time.Second // a rule change applies at once (invalidated) or within this
	minFreeForLogs  = 0.20             // below 20 % free, logs are shed and metrics keep flowing
	diskCheckEvery  = 10 * time.Second
)

// handlers are the two HTTP surfaces of one server process.
type handlers struct {
	ingest    *ingesthttp.Handler
	dashboard *httpapi.API
}

// build wires every use case onto the store. pin is nil in CA mode.
func build(ctx context.Context, st *sqlite.Store, dataDir string, pin []byte, extraOrigins []string) (handlers, error) {
	hub := livehub.New()
	svc := ingest.NewService(st, ingest.DefaultWindow, time.Now)
	svc.SetPublisher(hub)
	redactor := redact.New(st, ruleCacheTTL, time.Now)
	disk := diskguard.New(dataDir, minFreeForLogs, diskCheckEvery)
	svc.SetLogPipeline(redactor, disk)
	enroll := identity.NewEnrollment(st, time.Now)
	auth := ingest.NewAuthenticator(st, authTTL, authCache, time.Now)
	ingestH := ingesthttp.NewHandler(auth, svc, enroll)

	limiter := identity.NewLoginLimiter(loginFailures, loginWindow, loginTrackKeys, time.Now)
	sessions, err := identity.NewSessions(ctx, st, st, password.New(hashParallel), limiter, time.Now)
	if err != nil {
		return handlers{}, err
	}
	dist, err := web.Dist()
	if errors.Is(err, web.ErrNotBuilt) {
		slog.Warn("serving the API only", "reason", err) // the binary still works
		dist = nil
	} else if err != nil {
		return handlers{}, err
	}
	janitor := startJanitor(ctx, st)
	api := httpapi.New(httpapi.Config{
		Sessions: sessions, Users: st, Tokens: enroll, Hosts: st,
		Series: query.NewMetrics(st, st, st), Details: query.NewDetails(st, st), Procs: st, Live: hub,
		Logs: query.NewLogs(st), Rules: redaction.NewRules(st, redactor),
		Alerts: alerting.NewAdmin(st, st, st, st), HostAdmin: identity.NewHosts(st, auth, time.Now),
		Ready: st.Ready, System: systemReport(dataDir, st, janitor, disk, hub),
		Plans:        identity.NewPlans(st, time.Now),
		FormatToken:  func(secret []byte) string { return wire.Token{Secret: secret, Pin: pin}.String() },
		Web:          dist,
		ExtraOrigins: extraOrigins,
		Now:          time.Now,
	})
	startAlerting(ctx, st)
	return handlers{ingest: ingestH, dashboard: api}, nil
}
