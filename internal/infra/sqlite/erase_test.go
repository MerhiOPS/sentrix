package sqlite

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/compliance"
	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// eraseWorld is two tenants, each with two hosts full of data and a user.
type eraseWorld struct {
	st     *Store
	tenant [2]domain.TenantID
	hosts  [2][2]domain.HostID
	users  [2]domain.UserID
}

func newEraseWorld(t *testing.T) *eraseWorld {
	t.Helper()
	ctx := context.Background()
	w := &eraseWorld{st: newMigratedStore(t)}
	ops, _ := domain.NewOpsScope("test")
	now := time.Now().UTC().Truncate(time.Second)
	for ti := range 2 {
		w.tenant[ti] = domain.NewTenantID()
		if err := w.st.CreateTenant(ctx, ops, domain.Tenant{ID: w.tenant[ti], Name: fmt.Sprintf("tenant-%d", ti), CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
		sc, _ := domain.NewScope(w.tenant[ti])
		w.users[ti] = domain.NewUserID()
		if err := w.st.CreateUser(ctx, sc, domain.User{ID: w.users[ti], Tenant: w.tenant[ti], Email: fmt.Sprintf("person%d-%s@example.com", ti, w.users[ti]),
			Role: domain.RoleAdmin, CreatedAt: now}, "argon2id$fake-hash-"+w.users[ti].String()); err != nil {
			t.Fatal(err)
		}
		for hi := range 2 {
			w.hosts[ti][hi] = w.seedHost(t, sc, now, fmt.Sprintf("t%dh%d", ti, hi))
		}
	}
	return w
}

func (w *eraseWorld) seedHost(t *testing.T, sc domain.Scope, now time.Time, name string) domain.HostID {
	t.Helper()
	ctx := context.Background()
	secret := identity.NewSecret()
	tok := identity.EnrollmentToken{SecretHash: identity.HashSecret(secret), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := w.st.CreateToken(ctx, sc, tok); err != nil {
		t.Fatal(err)
	}
	h, err := w.st.RedeemToken(ctx, tok.SecretHash, now, identity.NewHost{Host: domain.Host{ID: domain.NewHostID(), Name: name, OS: "linux",
		Interval: 5 * time.Second, CreatedAt: now}, CredentialHash: identity.HashSecret(identity.NewSecret())})
	if err != nil {
		t.Fatal(err)
	}
	for seq := uint64(1); seq <= 20; seq++ { // interleaved with the other hosts' rows, so pages are shared
		at := now.Add(-time.Duration(seq) * time.Minute)
		_, err := w.st.CommitBatch(ctx, sc, h.ID, seq, func(st ingest.TxState) (ingest.Prepared, error) {
			return ingest.Prepared{Counters: st.Counters, Interval: 5 * time.Second, ReceivedAt: now, SeqRetention: time.Hour,
				Samples:  []domain.CoreSample{{TS: at, CPUPercent: 12}},
				Details:  []domain.DetailSample{{TS: at, Metric: domain.MetricCPUCore, Kind: domain.KindGauge, Labels: domain.Labels{"core": "0"}, Value: 3}},
				Logs:     []domain.LogLine{{TS: at, Source: "app", Level: domain.LevelInfo, Message: name + " says hello " + fmt.Sprint(seq)}},
				LogStats: ingest.LogStats{Received: 1, Bytes: 20},
			}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return h.ID
}

func erase(t *testing.T, st compliance.ErasureStore, audit compliance.AuditLog, s compliance.Subject) (compliance.Report, error) {
	t.Helper()
	return compliance.NewEraser(st, audit, time.Now).Erase(context.Background(), s)
}

func TestEraseHostLeavesNothingAndTouchesNothingElse(t *testing.T) {
	w := newEraseWorld(t)
	ctx := context.Background()
	rep, err := erase(t, w.st, w.st, compliance.Subject{Kind: compliance.SubjectHost, Tenant: w.tenant[0], Host: w.hosts[0][0]})
	if err != nil || len(rep.Residue) != 0 || rep.Needles != 3 { // canary, id, credential hash
		t.Fatalf("erase host: %v, residue %v, %d needles", err, rep.Residue, rep.Needles)
	}
	sa, _ := domain.NewScope(w.tenant[0])
	if _, err := w.st.GetHost(ctx, sa, w.hosts[0][0]); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("erased host still readable: %v", err)
	}
	for _, k := range [][2]int{{0, 1}, {1, 0}, {1, 1}} {
		sc, _ := domain.NewScope(w.tenant[k[0]])
		ss, err := w.st.RawCore(ctx, sc, w.hosts[k[0]][k[1]], time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		if err != nil || len(ss) != 20 {
			t.Fatalf("host %v lost data in someone else's erasure: %d, %v", k, len(ss), err)
		}
	}
	var audits int
	_ = w.st.db.Reader().QueryRow(`SELECT count(*) FROM audit_log WHERE action = 'erase_host' AND outcome = 'ok'`).Scan(&audits)
	if audits != 1 {
		t.Fatalf("%d audit entries, want 1", audits)
	}
}

func TestEraseUserAndTenant(t *testing.T) {
	w := newEraseWorld(t)
	if _, err := erase(t, w.st, w.st, compliance.Subject{Kind: compliance.SubjectUser, Tenant: w.tenant[1], User: w.users[1]}); err != nil {
		t.Fatalf("erase user: %v", err)
	}
	if _, err := erase(t, w.st, w.st, compliance.Subject{Kind: compliance.SubjectTenant, Tenant: w.tenant[0]}); err != nil {
		t.Fatalf("erase tenant: %v", err)
	}
	sb, _ := domain.NewScope(w.tenant[1])
	if hs, err := w.st.ListHosts(context.Background(), sb); err != nil || len(hs) != 2 {
		t.Fatalf("tenant B after A's erasure: %d hosts, %v", len(hs), err)
	}
	if _, err := erase(t, w.st, w.st, compliance.Subject{Kind: compliance.SubjectHost, Tenant: w.tenant[1], Host: w.hosts[0][1]}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("erasing tenant A's host under tenant B: %v, want ErrNotFound", err)
	}
}

// leaky copies the canary into another tenant's logs before the erasure: a
// row left behind. The post-check must catch it.
type leaky struct {
	*Store
	into domain.Scope
	host domain.HostID
}

func (l *leaky) PrepareErasure(ctx context.Context, ops domain.OpsScope, s compliance.Subject, canary string) ([][]byte, error) {
	needles, err := l.Store.PrepareErasure(ctx, ops, s, canary)
	if err != nil {
		return nil, err
	}
	_, err = l.CommitBatch(ctx, l.into, l.host, 999, func(st ingest.TxState) (ingest.Prepared, error) {
		return ingest.Prepared{Counters: st.Counters, Interval: 5 * time.Second, ReceivedAt: time.Now(), SeqRetention: time.Hour,
			Logs: []domain.LogLine{{TS: time.Now(), Source: "copy", Level: domain.LevelInfo, Message: "copied: " + canary}}}, nil
	})
	return needles, err
}

func TestPostCheckFailsWhenARowIsLeftBehind(t *testing.T) {
	w := newEraseWorld(t)
	sb, _ := domain.NewScope(w.tenant[1])
	rep, err := erase(t, &leaky{Store: w.st, into: sb, host: w.hosts[1][0]}, w.st,
		compliance.Subject{Kind: compliance.SubjectHost, Tenant: w.tenant[0], Host: w.hosts[0][0]})
	if !errors.Is(err, compliance.ErrResidue) || len(rep.Residue) == 0 {
		t.Fatalf("a leftover copy of the canary passed the post-check: %v %v", err, rep.Residue)
	}
	var failed int
	_ = w.st.db.Reader().QueryRow(`SELECT count(*) FROM audit_log WHERE outcome = 'failed'`).Scan(&failed)
	if failed != 1 {
		t.Fatal("the failed erasure was not audited as failed")
	}
}

func TestPostCheckFailsWithSecureDeleteOff(t *testing.T) {
	w := newEraseWorld(t)
	// The writer pool has one connection: this turns secure_delete off for
	// every statement that follows, as a misconfigured build would.
	if _, err := w.st.db.Writer().Exec(`PRAGMA secure_delete = 0`); err != nil {
		t.Fatal(err)
	}
	rep, err := erase(t, w.st, w.st, compliance.Subject{Kind: compliance.SubjectHost, Tenant: w.tenant[0], Host: w.hosts[0][0]})
	if !errors.Is(err, compliance.ErrResidue) {
		t.Fatalf("with secure_delete off the post-check passed (%v): it would not catch deleted rows in free space", err)
	}
	if !strings.Contains(strings.Join(rep.Residue, " "), "sentrix.db") && !strings.Contains(strings.Join(rep.Residue, " "), "s.db") {
		t.Fatalf("residue %v: expected it in the database file", rep.Residue)
	}
}

// The canary really lands where the subject's data lives — searchable in
// its logs and in its latest process snapshot — before anything is deleted.
func TestCanaryIsPlantedInLogsAndProcesses(t *testing.T) {
	w := newEraseWorld(t)
	ctx := context.Background()
	ops, _ := domain.NewOpsScope("test")
	sub := compliance.Subject{Kind: compliance.SubjectHost, Tenant: w.tenant[0], Host: w.hosts[0][0]}
	const canary = "sentrix-erasure-canary-planted-check"
	if _, err := w.st.PrepareErasure(ctx, ops, sub, canary); err != nil {
		t.Fatal(err)
	}
	sc, _ := domain.NewScope(w.tenant[0])
	clauses, _ := query.ParseSearch(canary)
	hits, _ := w.st.SearchLogs(ctx, sc, query.LogQuery{Clauses: clauses, From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour), Limit: 10})
	snap, err := w.st.ProcessesAt(ctx, sc, w.hosts[0][0], time.Now().Add(time.Minute))
	if len(hits) != 1 || err != nil || len(snap.Procs) != 1 || snap.Procs[0].Name != canary {
		t.Fatalf("canary not planted: %d log hits, snapshot %+v (%v)", len(hits), snap, err)
	}
}

// Residue reads the WAL too: data committed but not yet checkpointed lives
// only there.
func TestResidueScansTheWAL(t *testing.T) {
	st := newMigratedStore(t)
	ops, _ := domain.NewOpsScope("test")
	if err := st.CreateTenant(context.Background(), ops, domain.Tenant{ID: domain.NewTenantID(), Name: "only-in-the-wal-xyz", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	hits, err := st.Residue(context.Background(), ops, [][]byte{[]byte("only-in-the-wal-xyz")})
	if err != nil || len(hits) == 0 || !strings.Contains(strings.Join(hits, " "), "-wal") {
		t.Fatalf("hits %v, %v: a row still in the WAL must be found there", hits, err)
	}
}
