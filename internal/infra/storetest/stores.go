// Package storetest is the storage contract: behaviour every adapter must
// have, run against SQLite and memstore, and shown to fail against a broken
// adapter (PLAN §1, §4.1). A behaviour belongs here, not in an adapter's own
// tests, whenever a second adapter would have to replicate it.
package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/alerting"
	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/app/redaction"
	"github.com/MerhiOPS/sentrix/internal/app/retention"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Stores is every port the contract covers. An adapter satisfies it with one
// type or a struct embedding several.
type Stores interface {
	identity.TenantRepo
	identity.EnrollmentRepo
	identity.UserRepo
	identity.SessionRepo
	ingest.CredentialStore
	ingest.Store
	query.HostReader
	query.CoreReader
	query.LatestReader
	query.RollupReader
	query.DetailReader
	query.ProcessReader
	query.LogReader
	redaction.RuleRepo
	identity.HostRepo
	alerting.RuleRepo
	alerting.ChannelRepo
	alerting.SilenceRepo
	alerting.StateRepo
	alerting.BucketReader
	alerting.TenantLister
	alerting.Sweeper
	retention.Store
}

// Factory builds a fresh, empty adapter for one test.
type Factory func(t *testing.T) Stores

// T0 is the fixed clock every contract case starts from.
var T0 = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// fixture is a tenant with one enrolled host (and its credential hash), one
// user and one session.
type fixture struct {
	scope    domain.Scope
	tenant   domain.TenantID
	host     domain.HostID
	credHash []byte
	user     domain.UserID
	session  []byte // token hash
}

func mustOps(t *testing.T) domain.OpsScope {
	t.Helper()
	o, err := domain.NewOpsScope("storetest")
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// newTenant creates a tenant and returns its scope.
func newTenant(t *testing.T, st Stores, name string) (domain.Scope, domain.TenantID) {
	t.Helper()
	tid := domain.NewTenantID()
	if err := st.CreateTenant(context.Background(), mustOps(t), domain.Tenant{ID: tid, Name: name, CreatedAt: T0}); err != nil {
		t.Fatal(err)
	}
	s, err := domain.NewScope(tid)
	if err != nil {
		t.Fatal(err)
	}
	return s, tid
}

// enroll issues a token in s and redeems it into a host.
func enroll(t *testing.T, st Stores, s domain.Scope, name string) (domain.Host, []byte) {
	t.Helper()
	ctx := context.Background()
	secret := identity.NewSecret()
	tok := identity.EnrollmentToken{SecretHash: identity.HashSecret(secret), CreatedAt: T0, ExpiresAt: T0.Add(time.Hour)}
	if err := st.CreateToken(ctx, s, tok); err != nil {
		t.Fatal(err)
	}
	cred := identity.HashSecret(identity.NewSecret())
	h, err := st.RedeemToken(ctx, tok.SecretHash, T0, identity.NewHost{
		Host: domain.Host{
			ID: domain.NewHostID(), Name: name, OS: "linux", Platform: "test",
			AgentVersion: "0.0.0", Interval: 5 * time.Second, CreatedAt: T0,
		},
		CredentialHash: cred,
	})
	if err != nil {
		t.Fatal(err)
	}
	return h, cred
}

func newFixture(t *testing.T, st Stores, name string) fixture {
	t.Helper()
	s, tid := newTenant(t, st, name)
	h, cred := enroll(t, st, s, name+"-host")
	u := newUser(t, st, s, name+"-"+domain.NewID().String()+"@example.com", domain.RoleAdmin)
	return fixture{scope: s, tenant: tid, host: h.ID, credHash: cred, user: u.ID, session: newSession(t, st, s, u)}
}

// prepared builds a BuildFunc that stores samples at the given offsets (in
// intervals of 5 s from T0) with a counter value each.
func prepared(at time.Time, offsets []int, rx []uint64) ingest.BuildFunc {
	return func(state ingest.TxState) (ingest.Prepared, error) {
		p := ingest.Prepared{
			Counters: state.Counters, Interval: 5 * time.Second, ReceivedAt: at,
			SeqRetention: ingest.DefaultWindow.SeqRetention(),
		}
		for i, off := range offsets {
			p.Samples = append(p.Samples, domain.CoreSample{
				TS: T0.Add(time.Duration(off) * 5 * time.Second), CPUPercent: 12.345, NetRxBytes: rx[i],
			})
		}
		return p, nil
	}
}
