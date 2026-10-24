package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// CreateUser implements identity.UserRepo.
func (s *Store) CreateUser(ctx context.Context, sc domain.Scope, u domain.User, passwordHash string) error {
	w := s.db.Writer()
	tk, err := tenantKey(ctx, w, sc)
	if err != nil {
		return err
	}
	_, err = w.ExecContext(ctx, `INSERT INTO users (tenant_key, id, email, password_hash, role, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, tk, encID(domain.ID(u.ID)), u.Email, passwordHash, string(u.Role), encTime(u.CreatedAt))
	if err != nil && isUniqueViolation(err) {
		return fmt.Errorf("user %s: %w", u.Email, domain.ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

const userColumns = `u.id, t.id, u.email, u.role, u.created_at, u.disabled_at`

// GetUser implements identity.UserRepo.
func (s *Store) GetUser(ctx context.Context, sc domain.Scope, id domain.UserID) (domain.User, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return domain.User{}, err
	}
	row := r.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users u JOIN tenants t ON t.key = u.tenant_key
		WHERE u.tenant_key = ? AND u.id = ?`, tk, encID(domain.ID(id)))
	u, err := scanUser(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, fmt.Errorf("user: %w", domain.ErrNotFound)
	}
	return u, err
}

// ListUsers implements identity.UserRepo, ordered by email.
func (s *Store) ListUsers(ctx context.Context, sc domain.Scope) ([]domain.User, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	rows, err := r.QueryContext(ctx, `SELECT `+userColumns+` FROM users u JOIN tenants t ON t.key = u.tenant_key
		WHERE u.tenant_key = ? ORDER BY u.email`, tk)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	var out []domain.User
	for rows.Next() {
		u, err := scanUser(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// FindLogin implements identity.UserRepo. Unscoped by necessity (allowlisted
// in the statement audit): the email is how the tenant is found.
func (s *Store) FindLogin(ctx context.Context, email string) (domain.User, string, error) {
	var hash string
	row := s.db.Reader().QueryRowContext(ctx, `SELECT `+userColumns+`, u.password_hash
		FROM users u JOIN tenants t ON t.key = u.tenant_key WHERE u.email = ?`, email)
	u, err := scanUser(func(dst ...any) error { return row.Scan(append(dst, &hash)...) })
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, "", fmt.Errorf("login: %w", domain.ErrNotFound)
	}
	return u, hash, err
}

func scanUser(scan func(...any) error) (domain.User, error) {
	var (
		rawID, rawTenant []byte
		u                domain.User
		role             string
		created          int64
		disabled         sql.NullInt64
	)
	if err := scan(&rawID, &rawTenant, &u.Email, &role, &created, &disabled); err != nil {
		return domain.User{}, err
	}
	id, err := decID(rawID)
	if err != nil {
		return domain.User{}, err
	}
	tid, err := decID(rawTenant)
	if err != nil {
		return domain.User{}, err
	}
	u.ID, u.Tenant, u.Role, u.CreatedAt = domain.UserID(id), domain.TenantID(tid), domain.Role(role), decTime(created)
	if disabled.Valid {
		d := decTime(disabled.Int64)
		u.DisabledAt = &d
	}
	return u, nil
}
