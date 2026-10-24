// Package e2e wires the real server stack and the real agent together:
// SQLite, the ingest handler behind TLS with a self-signed key, enrollment
// with the pin, and shipping. It sits outside the layered tree on purpose.
package e2e

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/agent"
	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/ingesthttp"
	"github.com/MerhiOPS/sentrix/internal/infra/sqlite"
	"github.com/MerhiOPS/sentrix/internal/infra/tlscert"
	"github.com/MerhiOPS/sentrix/internal/wire"
)

type stack struct {
	store  *sqlite.Store
	srv    *httptest.Server
	pin    []byte
	scope  domain.Scope
	enroll *identity.Enrollment
	hits   atomic.Int64 // requests that reached the handler
}

func newStack(t *testing.T) *stack {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlite.OpenMigrated(context.Background(), filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	st := sqlite.NewStore(db)
	tn, err := identity.NewTenants(st, time.Now).Create(context.Background(), "e2e")
	if err != nil {
		t.Fatal(err)
	}
	sc, _ := domain.NewScope(tn.ID)
	cert, err := tlscert.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	pin, _ := tlscert.LeafPin(cert)
	s := &stack{store: st, pin: pin, scope: sc, enroll: identity.NewEnrollment(st, time.Now)}
	h := ingesthttp.NewHandler(ingest.NewAuthenticator(st, time.Minute, 100, time.Now),
		ingest.NewService(st, ingest.DefaultWindow, time.Now), s.enroll)
	s.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		h.ServeHTTP(w, r)
	}))
	s.srv.TLS = tlscert.ServerConfig(cert)
	s.srv.StartTLS()
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stack) token(t *testing.T, pin []byte) string {
	t.Helper()
	secret, _, err := s.enroll.IssueToken(context.Background(), s.scope)
	if err != nil {
		t.Fatal(err)
	}
	return wire.Token{Secret: secret, Pin: pin}.String()
}

// fakeCollector produces valid samples with a rising counter.
type fakeCollector struct{ n atomic.Uint64 }

func (f *fakeCollector) Collect(context.Context) (domain.CoreSample, []error) {
	n := f.n.Add(1)
	return domain.CoreSample{TS: time.Now(), CPUPercent: 25, MemoryPercent: 50, NetRxBytes: n * 1000}, nil
}

func TestAgentEnrollsWithPinAndShipsToSQLite(t *testing.T) {
	s := newStack(t)
	stateDir := t.TempDir()
	id, err := agent.Enroll(context.Background(), stateDir, s.srv.URL, s.token(t, s.pin), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	cfg := agent.DefaultConfig(stateDir)
	cfg.Interval, cfg.BatchEvery = time.Second, 2*time.Second
	a, err := agent.New(cfg, id, &fakeCollector{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.Run(ctx) // returns after ctx ends, having sealed and drained

	hid, _ := domain.ParseHostID(id.HostID)
	ss, err := s.store.RawCore(context.Background(), s.scope, hid, time.Now().Add(-time.Minute), time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) < 4 {
		t.Fatalf("only %d samples reached SQLite in 5 s at 1 s interval", len(ss))
	}
	for i := 1; i < len(ss); i++ {
		if ss[i].NetRxBytes < ss[i-1].NetRxBytes {
			t.Fatalf("stored counter went backwards: %d then %d", ss[i-1].NetRxBytes, ss[i].NetRxBytes)
		}
	}
	h, _ := s.store.GetHost(context.Background(), s.scope, hid)
	if h.Status(time.Now()) != domain.StatusOK {
		t.Fatalf("host status after shipping: %s", h.Status(time.Now()))
	}
}

func TestEnrollRefusesWrongPinBeforeSendingSecret(t *testing.T) {
	s := newStack(t)
	otherCert, err := tlscert.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wrongPin, _ := tlscert.LeafPin(otherCert)
	_, err = agent.Enroll(context.Background(), t.TempDir(), s.srv.URL, s.token(t, wrongPin), time.Second)
	if err == nil || !errors.Is(err, tlscert.ErrPinMismatch) && !strings.Contains(err.Error(), "does not match the pinned key") {
		t.Fatalf("enroll against a different key: err = %v, want pin mismatch", err)
	}
	if n := s.hits.Load(); n != 0 {
		t.Fatalf("%d request(s) reached the server: the token secret was sent before the pin check", n)
	}
}
