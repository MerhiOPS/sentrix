package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/compliance"
	"github.com/MerhiOPS/sentrix/internal/app/uptime"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

const (
	privateTarget = "https://t0-private.example.com/orders?customer=4711"
	sharedTarget  = "https://shared.example.com/"
)

// seedMonitors gives tenant 0 a monitor with a target only it uses, and
// both tenants a monitor of the same public target, each with results.
func (w *eraseWorld) seedMonitors(t *testing.T) {
	t.Helper()
	for ti, targets := range [][]string{{privateTarget, sharedTarget}, {sharedTarget}} {
		sc, _ := domain.NewScope(w.tenant[ti])
		for _, target := range targets {
			m := domain.Monitor{Name: "site", Kind: domain.MonitorHTTP, Target: target, Interval: time.Minute,
				Timeout: 5 * time.Second, Method: "GET", CreatedAt: time.Now()}
			id, err := w.st.CreateMonitor(context.Background(), sc, m, 10)
			if err != nil {
				t.Fatal(err)
			}
			m.ID = id
			prev := domain.MonitorState{Monitor: id, Status: domain.MonitorPending}
			r := domain.CheckResult{At: time.Now(), Outcome: domain.CheckDown, Class: domain.ErrTimeout}
			if _, err := w.st.RecordCheck(context.Background(), sc, uptime.CheckRecord{Monitor: m, Result: r,
				Step: domain.StepMonitor(m, prev, r)}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestEraseTenantRemovesItsMonitors(t *testing.T) {
	w := newEraseWorld(t)
	w.seedMonitors(t)
	rep, err := erase(t, w.st, w.st, compliance.Subject{Kind: compliance.SubjectTenant, Tenant: w.tenant[0]})
	if err != nil {
		t.Fatalf("erase tenant with monitors: %v (residue %v): a target another tenant also monitors is not residue", err, rep.Residue)
	}
	var left int
	if err := w.st.db.Reader().QueryRow(`SELECT count(*) FROM monitor_results`).Scan(&left); err != nil || left != 1 {
		t.Fatalf("%d results remain (%v), want only tenant B's one", left, err)
	}
	sb, _ := domain.NewScope(w.tenant[1])
	if ms, err := w.st.ListMonitors(context.Background(), sb); err != nil || len(ms) != 1 || ms[0].Target != sharedTarget {
		t.Fatalf("tenant B's monitors after A's erasure: %+v, %v", ms, err)
	}
}

// leakyMonitor copies tenant A's private monitor URL into tenant B's
// monitor before the erasure: a monitor URL left behind.
type leakyMonitor struct {
	*Store
	into domain.Scope
}

func (l *leakyMonitor) PrepareErasure(ctx context.Context, ops domain.OpsScope, s compliance.Subject, canary string) ([][]byte, error) {
	needles, err := l.Store.PrepareErasure(ctx, ops, s, canary)
	if err != nil {
		return nil, err
	}
	ms, err := l.ListMonitors(ctx, l.into)
	if err != nil || len(ms) == 0 {
		return nil, errors.Join(err, errors.New("no monitor to copy into"))
	}
	ms[0].Name = "copied " + privateTarget
	return needles, l.UpdateMonitor(ctx, l.into, ms[0], time.Now())
}

func TestPostCheckFailsWhenAMonitorURLIsLeftBehind(t *testing.T) {
	w := newEraseWorld(t)
	w.seedMonitors(t)
	sb, _ := domain.NewScope(w.tenant[1])
	rep, err := erase(t, &leakyMonitor{Store: w.st, into: sb}, w.st, compliance.Subject{Kind: compliance.SubjectTenant, Tenant: w.tenant[0]})
	if !errors.Is(err, compliance.ErrResidue) || len(rep.Residue) == 0 {
		t.Fatalf("a monitor URL left in another row passed the post-check: %v %v", err, rep.Residue)
	}
}
