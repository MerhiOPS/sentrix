package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/MerhiOPS/sentrix/internal/agent"
	"github.com/MerhiOPS/sentrix/internal/app/alerting"
	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/collector"
	"github.com/MerhiOPS/sentrix/internal/infra/logtail"
	"github.com/MerhiOPS/sentrix/internal/infra/password"
	"github.com/MerhiOPS/sentrix/internal/infra/sqlite"
	"github.com/MerhiOPS/sentrix/internal/wire"
)

// --demo seeds a tenant and runs simulated hosts through the real product:
// each one is the real agent (batching, disk WAL, pinned TLS, enrollment)
// fed by simulated collectors, shipping to this server's own ingest port.
// Nothing is written to storage behind the API's back.

const (
	demoFile  = "demo.json"
	demoEmail = "demo@sentrix.local"
)

var demoHosts = []collector.HostInfo{
	{Hostname: "web-01", OS: "linux", Platform: "ubuntu 24.04"},
	{Hostname: "web-02", OS: "linux", Platform: "ubuntu 24.04"},
	{Hostname: "db-01", OS: "linux", Platform: "debian 12"},
	{Hostname: "worker-01", OS: "windows", Platform: "Windows Server 2022"},
}

type demoState struct {
	Tenant   string `json:"tenant"`
	Password string `json:"password"`
}

// setupDemo creates the demo tenant, its admin and a sample alert rule on
// first start; later starts reuse them.
func setupDemo(ctx context.Context, st *sqlite.Store, dataDir string) (demoState, error) {
	path := filepath.Join(dataDir, demoFile)
	var s demoState
	if b, err := os.ReadFile(path); err == nil { //nolint:gosec // inside the data dir
		return s, json.Unmarshal(b, &s)
	}
	tn, err := identity.NewTenants(st, time.Now).Create(ctx, "Demo")
	if err != nil {
		return s, err
	}
	sc, _ := domain.NewScope(tn.ID)
	pw := make([]byte, 12)
	if _, err := rand.Read(pw); err != nil {
		return s, err
	}
	s = demoState{Tenant: tn.ID.String(), Password: base64.RawURLEncoding.EncodeToString(pw)}
	if _, err := identity.NewUsers(st, password.New(1), time.Now).Create(ctx, sc, demoEmail, s.Password, domain.RoleAdmin); err != nil {
		return s, err
	}
	_, err = alerting.NewAdmin(st, st, st, st).CreateRule(ctx, sc, domain.AlertRule{Name: "CPU above 85 %", Kind: domain.RuleThreshold,
		Metric: "cpu", Aggregation: domain.AggAvg, Comparator: domain.CmpGT, Threshold: 85, Duration: 2 * time.Minute,
		OnNoData: domain.NoDataHold, Cooldown: 15 * time.Minute, Enabled: true})
	if err != nil {
		return s, err
	}
	b, _ := json.Marshal(s)
	return s, os.WriteFile(path, b, 0o600)
}

// runDemo enrolls (once) and runs the simulated hosts until ctx ends.
func runDemo(ctx context.Context, st *sqlite.Store, dataDir, ingestListen string, pin []byte, s demoState) error {
	tid, err := domain.ParseTenantID(s.Tenant)
	if err != nil {
		return err
	}
	sc, _ := domain.NewScope(tid)
	_, port, err := net.SplitHostPort(ingestListen)
	if err != nil {
		return err
	}
	server := "https://" + net.JoinHostPort("127.0.0.1", port)
	log := slog.Default().With("component", "demo")
	for i, hi := range demoHosts {
		dir := filepath.Join(dataDir, "demo-agents", hi.Hostname)
		id, err := agent.LoadIdentity(dir)
		if errors.Is(err, agent.ErrNotEnrolled) {
			id, err = enrollDemo(ctx, st, sc, dir, server, pin, hi)
		}
		if err != nil {
			return fmt.Errorf("demo host %s: %w", hi.Hostname, err)
		}
		cfg := agent.DefaultConfig(dir)
		core := newSimCore(i)
		a, err := agent.New(cfg, id, agent.Sources{Core: core, Detail: &simDetail{core: core}, Procs: simProcs{},
			Logs: []logtail.Source{&simLogs{host: hi.Hostname}}}, log.With("host", hi.Hostname))
		if err != nil {
			return err
		}
		go a.Run(ctx)
	}
	log.Info("demo running", "hosts", len(demoHosts), "login", demoEmail, "password", s.Password)
	return nil
}

// enrollDemo issues a token and enrolls through the ingest endpoint,
// retrying while the listener comes up.
func enrollDemo(ctx context.Context, st *sqlite.Store, sc domain.Scope, dir, server string, pin []byte, hi collector.HostInfo) (agent.Identity, error) {
	secret, _, err := identity.NewEnrollment(st, time.Now).IssueToken(ctx, sc)
	if err != nil {
		return agent.Identity{}, err
	}
	token := wire.Token{Secret: secret, Pin: pin}.String()
	var lastErr error
	for range 60 {
		id, err := agent.EnrollAs(ctx, dir, server, token, 5*time.Second, hi)
		if err == nil {
			return id, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return agent.Identity{}, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return agent.Identity{}, lastErr
}
