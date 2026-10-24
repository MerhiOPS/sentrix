// Package compliance holds the operator use cases that act on a tenant's
// data as a whole: erasure (GDPR), and the audit log they write.
package compliance

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// SubjectKind is what is erased.
type SubjectKind string

const (
	SubjectHost   SubjectKind = "host"
	SubjectUser   SubjectKind = "user"
	SubjectTenant SubjectKind = "tenant"
)

// Subject identifies what to erase. Host and User are set for their kinds.
type Subject struct {
	Kind   SubjectKind
	Tenant domain.TenantID
	Host   domain.HostID
	User   domain.UserID
}

// id is the subject's own identifier.
func (s Subject) id() domain.ID {
	switch s.Kind {
	case SubjectHost:
		return domain.ID(s.Host)
	case SubjectUser:
		return domain.ID(s.User)
	default:
		return domain.ID(s.Tenant)
	}
}

// ErasureStore performs the storage half of an erasure (PLAN §4.3).
type ErasureStore interface {
	// PrepareErasure checks the subject exists, plants canary in the
	// subject's own data, and returns every byte string that must be gone
	// afterwards (the canary, the subject's id, identifying values).
	PrepareErasure(ctx context.Context, ops domain.OpsScope, s Subject, canary string) ([][]byte, error)
	// DeleteSubject deletes the subject and everything that belongs to it.
	DeleteSubject(ctx context.Context, ops domain.OpsScope, s Subject) error
	// Purge removes what DELETE leaves behind: FTS segments, free pages,
	// WAL frames. It fails if the WAL cannot be truncated.
	Purge(ctx context.Context, ops domain.OpsScope) error
	// Residue byte-scans the stored files and reports where any needle is.
	Residue(ctx context.Context, ops domain.OpsScope, needles [][]byte) ([]string, error)
}

// AuditEntry is one operator action.
type AuditEntry struct {
	At          time.Time
	Action      string
	SubjectHash string // SHA-256 of the subject id: the id itself must not survive its erasure
	Outcome     string // "ok" | "failed"
	Detail      string
}

// AuditLog records operator actions.
type AuditLog interface {
	AppendAudit(ctx context.Context, ops domain.OpsScope, e AuditEntry) error
}

// ErrResidue means the post-check found the subject's data after erasure.
var ErrResidue = errors.New("erasure post-check failed: subject data still on disk")

// Report is what an erasure did.
type Report struct {
	Subject     Subject
	SubjectHash string
	Needles     int
	Residue     []string
}

// Eraser runs erasures.
type Eraser struct {
	store ErasureStore
	audit AuditLog
	now   func() time.Time
}

// NewEraser builds the use case.
func NewEraser(store ErasureStore, audit AuditLog, now func() time.Time) *Eraser {
	return &Eraser{store: store, audit: audit, now: now}
}

// Erase deletes the subject and proves it: a canary planted in the
// subject's data beforehand must not be found anywhere in the database or
// its WAL afterwards. Every run is audited, success or not.
func (e *Eraser) Erase(ctx context.Context, s Subject) (Report, error) {
	id := s.id()
	sum := sha256.Sum256(id[:])
	rep := Report{Subject: s, SubjectHash: hex.EncodeToString(sum[:])}
	ops, err := domain.NewOpsScope("erase " + string(s.Kind))
	if err != nil {
		return rep, err
	}
	err = e.run(ctx, ops, s, &rep)
	entry := AuditEntry{At: e.now().UTC(), Action: "erase_" + string(s.Kind), SubjectHash: rep.SubjectHash, Outcome: "ok"}
	if err != nil {
		entry.Outcome, entry.Detail = "failed", err.Error()
	}
	if aerr := e.audit.AppendAudit(ctx, ops, entry); aerr != nil {
		return rep, errors.Join(err, fmt.Errorf("audit: %w", aerr))
	}
	return rep, err
}

func (e *Eraser) run(ctx context.Context, ops domain.OpsScope, s Subject, rep *Report) error {
	canary, err := newCanary()
	if err != nil {
		return err
	}
	needles, err := e.store.PrepareErasure(ctx, ops, s, canary)
	if err != nil {
		return err
	}
	rep.Needles = len(needles)
	if err := e.store.DeleteSubject(ctx, ops, s); err != nil {
		return err
	}
	if err := e.store.Purge(ctx, ops); err != nil {
		return err
	}
	if rep.Residue, err = e.store.Residue(ctx, ops, needles); err != nil {
		return err
	}
	if len(rep.Residue) > 0 {
		return fmt.Errorf("%w: %s", ErrResidue, strings.Join(rep.Residue, "; "))
	}
	return nil
}

func newCanary() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("canary: %w", err)
	}
	return "sentrix-erasure-canary-" + hex.EncodeToString(b), nil
}
