package compliance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// fakeStore records the erasure steps in order and fails the one named.
type fakeStore struct {
	failAt  string
	residue []string
	steps   []string
	canary  string
	needles [][]byte
	checked [][]byte
}

var errStep = errors.New("step failed")

func (f *fakeStore) step(name string) error {
	f.steps = append(f.steps, name)
	if f.failAt == name {
		return errStep
	}
	return nil
}

func (f *fakeStore) PrepareErasure(_ context.Context, _ domain.OpsScope, s Subject, canary string) ([][]byte, error) {
	f.canary = canary
	id := s.id()
	f.needles = [][]byte{[]byte(canary), []byte(id.String())}
	return f.needles, f.step("prepare")
}
func (f *fakeStore) DeleteSubject(context.Context, domain.OpsScope, Subject) error {
	return f.step("delete")
}
func (f *fakeStore) Purge(context.Context, domain.OpsScope) error { return f.step("purge") }
func (f *fakeStore) Residue(_ context.Context, _ domain.OpsScope, needles [][]byte) ([]string, error) {
	f.checked = needles
	return f.residue, f.step("residue")
}

type fakeAudit struct {
	entries []AuditEntry
	err     error
}

func (a *fakeAudit) AppendAudit(_ context.Context, _ domain.OpsScope, e AuditEntry) error {
	a.entries = append(a.entries, e)
	return a.err
}

var fixedNow = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func newEraser(st *fakeStore, au *fakeAudit) *Eraser {
	return NewEraser(st, au, func() time.Time { return fixedNow })
}

func TestEraseRunsEveryStepInOrderAndAuditsOK(t *testing.T) {
	st, au := &fakeStore{}, &fakeAudit{}
	host := domain.NewHostID()
	rep, err := newEraser(st, au).Erase(context.Background(), Subject{Kind: SubjectHost, Tenant: domain.NewTenantID(), Host: host})
	if err != nil {
		t.Fatalf("erase: %v", err)
	}
	if got := strings.Join(st.steps, ","); got != "prepare,delete,purge,residue" {
		t.Fatalf("steps %s, want prepare,delete,purge,residue", got)
	}
	if len(st.checked) != len(st.needles) || string(st.checked[0]) != st.canary {
		t.Fatalf("post-check scanned %q, want the needles from prepare, canary first", st.checked)
	}
	if !strings.HasPrefix(st.canary, "sentrix-erasure-canary-") || len(st.canary) < len("sentrix-erasure-canary-")+32 {
		t.Fatalf("canary %q is not a random, recognisable marker", st.canary)
	}
	if rep.Needles != 2 {
		t.Fatalf("report needles %d, want 2", rep.Needles)
	}
	if len(au.entries) != 1 {
		t.Fatalf("%d audit entries, want 1", len(au.entries))
	}
	e := au.entries[0]
	id := domain.ID(host)
	sum := sha256.Sum256(id[:])
	if e.Action != "erase_host" || e.Outcome != "ok" || e.SubjectHash != hex.EncodeToString(sum[:]) || !e.At.Equal(fixedNow) {
		t.Fatalf("audit entry %+v", e)
	}
	if strings.Contains(e.SubjectHash+e.Detail, host.String()) {
		t.Fatal("the audit entry carries the erased subject's id")
	}
}

func TestEraseUsesAFreshCanaryEveryRun(t *testing.T) {
	st, au := &fakeStore{}, &fakeAudit{}
	e := newEraser(st, au)
	s := Subject{Kind: SubjectTenant, Tenant: domain.NewTenantID()}
	if _, err := e.Erase(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	first := st.canary
	if _, err := e.Erase(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if st.canary == first {
		t.Fatal("two erasures planted the same canary: a leftover from the first would pass the second")
	}
}

func TestEraseFailsAndAuditsWhenResidueIsFound(t *testing.T) {
	st, au := &fakeStore{residue: []string{"main db at offset 4096"}}, &fakeAudit{}
	rep, err := newEraser(st, au).Erase(context.Background(), Subject{Kind: SubjectUser, Tenant: domain.NewTenantID(), User: domain.NewUserID()})
	if !errors.Is(err, ErrResidue) {
		t.Fatalf("err %v, want ErrResidue", err)
	}
	if len(rep.Residue) != 1 || !strings.Contains(err.Error(), "offset 4096") {
		t.Fatalf("residue %v / %v not reported", rep.Residue, err)
	}
	if e := au.entries[0]; e.Outcome != "failed" || e.Action != "erase_user" || !strings.Contains(e.Detail, "post-check") {
		t.Fatalf("audit entry %+v, want a failed erase_user with the post-check detail", e)
	}
}

func TestEraseStopsAtTheFailingStepAndAuditsIt(t *testing.T) {
	cases := []struct {
		failAt string
		ran    string
	}{
		{"prepare", "prepare"},
		{"delete", "prepare,delete"},
		{"purge", "prepare,delete,purge"},
		{"residue", "prepare,delete,purge,residue"},
	}
	for _, c := range cases {
		t.Run(c.failAt, func(t *testing.T) {
			st, au := &fakeStore{failAt: c.failAt}, &fakeAudit{}
			_, err := newEraser(st, au).Erase(context.Background(), Subject{Kind: SubjectTenant, Tenant: domain.NewTenantID()})
			if !errors.Is(err, errStep) {
				t.Fatalf("err %v, want the step's error", err)
			}
			if got := strings.Join(st.steps, ","); got != c.ran {
				t.Fatalf("steps %s, want %s: nothing may run after a failed step", got, c.ran)
			}
			if len(au.entries) != 1 || au.entries[0].Outcome != "failed" {
				t.Fatalf("audit %+v, want one failed entry", au.entries)
			}
		})
	}
}

func TestEraseReportsAnAuditFailureAlongsideTheResult(t *testing.T) {
	auditErr := errors.New("audit table locked")
	cases := []struct {
		name    string
		failAt  string
		wantErr []error
	}{
		{"after success", "", []error{auditErr}},
		{"after a failed step", "delete", []error{auditErr, errStep}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, au := &fakeStore{failAt: c.failAt}, &fakeAudit{err: auditErr}
			_, err := newEraser(st, au).Erase(context.Background(), Subject{Kind: SubjectTenant, Tenant: domain.NewTenantID()})
			for _, want := range c.wantErr {
				if !errors.Is(err, want) {
					t.Fatalf("err %v does not carry %v", err, want)
				}
			}
		})
	}
}
