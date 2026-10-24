package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"

	"github.com/MerhiOPS/sentrix/internal/app/compliance"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// PrepareErasure implements compliance.ErasureStore. The canary goes where
// the subject's data lives — a log line (and so the search index) and a
// process snapshot for a host; a session for a user; the tenant row and
// each of its hosts for a tenant — and the needles add the identifiers
// that must vanish with it.
func (s *Store) PrepareErasure(ctx context.Context, ops domain.OpsScope, sub compliance.Subject, canary string) ([][]byte, error) {
	if _, err := ops.Reason(); err != nil {
		return nil, err
	}
	var needles [][]byte
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		sc, err := domain.NewScope(sub.Tenant)
		if err != nil {
			return err
		}
		tk, err := tenantKey(ctx, tx, sc)
		if err != nil {
			return err
		}
		needles = [][]byte{[]byte(canary)}
		switch sub.Kind {
		case compliance.SubjectHost:
			hk, err := hostKey(ctx, tx, tk, sub.Host)
			if err != nil {
				return err
			}
			n, err := plantHost(ctx, tx, tk, hk, canary, s.now().UnixMilli())
			needles = append(needles, n...)
			return err
		case compliance.SubjectUser:
			n, err := plantUser(ctx, tx, tk, sub.User, canary, s.now().UnixMilli())
			needles = append(needles, n...)
			return err
		case compliance.SubjectTenant:
			n, err := plantTenant(ctx, tx, tk, canary, s.now().UnixMilli())
			needles = append(needles, encID(domain.ID(sub.Tenant)))
			needles = append(needles, n...)
			return err
		default:
			return fmt.Errorf("%w: subject kind %q", domain.ErrInvalid, sub.Kind)
		}
	})
	return needles, err
}

// plantHost writes the canary into a host's logs and processes and returns
// the host's id and credential hash as needles.
func plantHost(ctx context.Context, tx *sql.Tx, tk, hk int64, canary string, now int64) ([][]byte, error) {
	if _, err := tx.ExecContext(ctx, `INSERT INTO logs (tenant_key, host_key, ts, source, level, message, labels)
		VALUES (?, ?, ?, 'erasure-canary', 'info', ?, '{}')`, tk, hk, now, canary); err != nil {
		return nil, fmt.Errorf("plant log canary: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO process_snapshots (tenant_key, host_key, ts, procs)
		VALUES (?, ?, ?, ?)`, tk, hk, now, `[{"name":"`+canary+`","cpu":0,"rss":0}]`); err != nil {
		return nil, fmt.Errorf("plant process canary: %w", err)
	}
	var id, cred []byte
	if err := tx.QueryRowContext(ctx, `SELECT id, credential_hash FROM hosts WHERE tenant_key = ? AND key = ?`, tk, hk).
		Scan(&id, &cred); err != nil {
		return nil, fmt.Errorf("read host: %w", err)
	}
	return [][]byte{id, cred}, nil
}

// plantUser adds a session whose token hash derives from the canary, and
// returns the user's id, email and password hash as needles.
func plantUser(ctx context.Context, tx *sql.Tx, tk int64, user domain.UserID, canary string, now int64) ([][]byte, error) {
	var uk int64
	var email, pw string
	err := tx.QueryRowContext(ctx, `SELECT key, email, password_hash FROM users WHERE tenant_key = ? AND id = ?`,
		tk, encID(domain.ID(user))).Scan(&uk, &email, &pw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("user: %w", domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("read user: %w", err)
	}
	token := sha256.Sum256([]byte(canary))
	if _, err := tx.ExecContext(ctx, `INSERT INTO sessions (token_hash, tenant_key, user_key, created_at, last_seen)
		VALUES (?, ?, ?, ?, ?)`, token[:], tk, uk, now, now); err != nil {
		return nil, fmt.Errorf("plant session canary: %w", err)
	}
	return [][]byte{token[:], encID(domain.ID(user)), []byte(email), []byte(pw)}, nil
}

// plantTenant renames the tenant to the canary, plants it in every host,
// and returns every host's and user's identifiers as needles.
func plantTenant(ctx context.Context, tx *sql.Tx, tk int64, canary string, now int64) ([][]byte, error) {
	if _, err := tx.ExecContext(ctx, `UPDATE tenants SET name = ? WHERE key = ?`, canary, tk); err != nil {
		return nil, fmt.Errorf("plant tenant canary: %w", err)
	}
	var needles [][]byte
	hosts, err := keysOf(ctx, tx, `SELECT key FROM hosts WHERE tenant_key = ?`, tk)
	if err != nil {
		return nil, err
	}
	for _, hk := range hosts {
		n, err := plantHost(ctx, tx, tk, hk, canary, now)
		if err != nil {
			return nil, err
		}
		needles = append(needles, n...)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, email FROM users WHERE tenant_key = ?`, tk)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id []byte
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		needles = append(needles, id, []byte(email))
	}
	return needles, rows.Err()
}

func keysOf(ctx context.Context, tx *sql.Tx, q string, arg int64) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, q, arg)
	if err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var k int64
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// DeleteSubject implements compliance.ErasureStore. Every table holding a
// host's, user's or tenant's rows references it with ON DELETE CASCADE
// (and the logs delete trigger removes the FTS rows), so one DELETE takes
// all of it; the erasure tests prove nothing is left.
func (s *Store) DeleteSubject(ctx context.Context, ops domain.OpsScope, sub compliance.Subject) error {
	if _, err := ops.Reason(); err != nil {
		return err
	}
	w := s.db.Writer()
	sc, err := domain.NewScope(sub.Tenant)
	if err != nil {
		return err
	}
	tk, err := tenantKey(ctx, w, sc)
	if err != nil {
		return err
	}
	var res sql.Result
	switch sub.Kind {
	case compliance.SubjectHost:
		res, err = w.ExecContext(ctx, `DELETE FROM hosts WHERE tenant_key = ? AND id = ?`, tk, encID(domain.ID(sub.Host)))
	case compliance.SubjectUser:
		res, err = w.ExecContext(ctx, `DELETE FROM users WHERE tenant_key = ? AND id = ?`, tk, encID(domain.ID(sub.User)))
	case compliance.SubjectTenant:
		res, err = w.ExecContext(ctx, `DELETE FROM tenants WHERE key = ?`, tk)
	default:
		return fmt.Errorf("%w: subject kind %q", domain.ErrInvalid, sub.Kind)
	}
	if err != nil {
		return fmt.Errorf("delete %s: %w", sub.Kind, err)
	}
	s.series.forgetAll() // cached series keys may point at deleted rows
	return mustAffect(res, string(sub.Kind))
}

// Purge implements compliance.ErasureStore (PLAN §4.3): merge the FTS index
// so deleted documents leave its segments, return free pages, and fold the
// WAL into the database and truncate it, so no old frame still holds the
// deleted rows. secure_delete (always on for the writer) zeroes the freed
// content itself.
func (s *Store) Purge(ctx context.Context, ops domain.OpsScope) error {
	if _, err := ops.Reason(); err != nil {
		return err
	}
	w := s.db.Writer()
	for _, q := range []string{`INSERT INTO logs_fts (logs_fts) VALUES ('optimize')`, `PRAGMA incremental_vacuum`} {
		if _, err := w.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("purge (%s): %w", q, err)
		}
	}
	var busy, logFrames, done int
	if err := w.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &done); err != nil {
		return fmt.Errorf("checkpoint: %w", err)
	}
	if busy != 0 {
		return errors.New("checkpoint could not truncate the WAL: a reader is active — stop the server and retry")
	}
	return nil
}

// Residue implements compliance.ErasureStore.
func (s *Store) Residue(_ context.Context, ops domain.OpsScope, needles [][]byte) ([]string, error) {
	if _, err := ops.Reason(); err != nil {
		return nil, err
	}
	return scanFiles(s.db.Path(), needles)
}

// AppendAudit implements compliance.AuditLog.
func (s *Store) AppendAudit(ctx context.Context, ops domain.OpsScope, e compliance.AuditEntry) error {
	if _, err := ops.Reason(); err != nil {
		return err
	}
	_, err := s.db.Writer().ExecContext(ctx, `INSERT INTO audit_log (at, action, subject_hash, outcome, detail)
		VALUES (?, ?, ?, ?, ?)`, encTime(e.At), e.Action, e.SubjectHash, e.Outcome, e.Detail)
	if err != nil {
		return fmt.Errorf("append audit: %w", err)
	}
	return nil
}
