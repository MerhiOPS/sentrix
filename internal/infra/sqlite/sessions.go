package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// CreateSession implements identity.SessionRepo.
func (s *Store) CreateSession(ctx context.Context, sc domain.Scope, sess domain.Session) error {
	w := s.db.Writer()
	tk, err := tenantKey(ctx, w, sc)
	if err != nil {
		return err
	}
	var uk int64
	err = w.QueryRowContext(ctx, `SELECT key FROM users WHERE tenant_key = ? AND id = ?`,
		tk, encID(domain.ID(sess.User))).Scan(&uk)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("session user: %w", domain.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("resolve session user: %w", err)
	}
	_, err = w.ExecContext(ctx, `INSERT INTO sessions (token_hash, tenant_key, user_key, created_at, last_seen)
		VALUES (?, ?, ?, ?, ?)`, sess.TokenHash, tk, uk, encTime(sess.CreatedAt), encTime(sess.LastSeen))
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// GetSession implements identity.SessionRepo. Unscoped by necessity
// (allowlisted in the statement audit): the cookie finds the tenant. The
// role is the user's current one; a disabled user has no sessions.
func (s *Store) GetSession(ctx context.Context, tokenHash []byte) (domain.Session, error) {
	var rawUser, rawTenant []byte
	var role string
	var created, seen int64
	err := s.db.Reader().QueryRowContext(ctx, `SELECT u.id, t.id, u.role, s.created_at, s.last_seen
		FROM sessions s JOIN users u ON u.key = s.user_key JOIN tenants t ON t.key = s.tenant_key
		WHERE s.token_hash = ? AND u.disabled_at IS NULL`, tokenHash).Scan(&rawUser, &rawTenant, &role, &created, &seen)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Session{}, fmt.Errorf("session: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.Session{}, fmt.Errorf("get session: %w", err)
	}
	uid, err := decID(rawUser)
	if err != nil {
		return domain.Session{}, err
	}
	tid, err := decID(rawTenant)
	if err != nil {
		return domain.Session{}, err
	}
	return domain.Session{
		TokenHash: append([]byte(nil), tokenHash...), User: domain.UserID(uid), Tenant: domain.TenantID(tid),
		Role: domain.Role(role), CreatedAt: decTime(created), LastSeen: decTime(seen),
	}, nil
}

// TouchSession implements identity.SessionRepo.
func (s *Store) TouchSession(ctx context.Context, sc domain.Scope, tokenHash []byte, at time.Time) error {
	return s.sessionExec(ctx, sc, `UPDATE sessions SET last_seen = ? WHERE tenant_key = ? AND token_hash = ?`,
		func(tk int64) []any { return []any{encTime(at), tk, tokenHash} })
}

// DeleteSession implements identity.SessionRepo (idempotent).
func (s *Store) DeleteSession(ctx context.Context, sc domain.Scope, tokenHash []byte) error {
	return s.sessionExec(ctx, sc, `DELETE FROM sessions WHERE tenant_key = ? AND token_hash = ?`,
		func(tk int64) []any { return []any{tk, tokenHash} })
}

func (s *Store) sessionExec(ctx context.Context, sc domain.Scope, q string, args func(int64) []any) error {
	w := s.db.Writer()
	tk, err := tenantKey(ctx, w, sc)
	if err != nil {
		return err
	}
	if _, err := w.ExecContext(ctx, q, args(tk)...); err != nil {
		return fmt.Errorf("session write: %w", err)
	}
	return nil
}
