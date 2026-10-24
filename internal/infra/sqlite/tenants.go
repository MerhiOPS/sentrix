package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// CreateTenant implements identity.TenantRepo.
func (s *Store) CreateTenant(ctx context.Context, ops domain.OpsScope, t domain.Tenant) error {
	if _, err := ops.Reason(); err != nil {
		return err
	}
	_, err := s.db.Writer().ExecContext(ctx,
		`INSERT INTO tenants (id, name, created_at) VALUES (?, ?, ?)`,
		encID(domain.ID(t.ID)), t.Name, encTime(t.CreatedAt))
	if err != nil && isUniqueViolation(err) {
		return fmt.Errorf("tenant %s: %w", t.ID, domain.ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("insert tenant: %w", err)
	}
	return nil
}

// GetTenant implements identity.TenantRepo.
func (s *Store) GetTenant(ctx context.Context, sc domain.Scope) (domain.Tenant, error) {
	tid, err := sc.Tenant()
	if err != nil {
		return domain.Tenant{}, err
	}
	var name string
	var created int64
	err = s.db.Reader().QueryRowContext(ctx,
		`SELECT name, created_at FROM tenants WHERE id = ?`, encID(domain.ID(tid))).Scan(&name, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Tenant{}, fmt.Errorf("tenant: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.Tenant{}, fmt.Errorf("get tenant: %w", err)
	}
	return domain.Tenant{ID: tid, Name: name, CreatedAt: decTime(created)}, nil
}

// SetSeriesLimit implements identity.TenantRepo.
func (s *Store) SetSeriesLimit(ctx context.Context, ops domain.OpsScope, t domain.TenantID, limit int) error {
	if _, err := ops.Reason(); err != nil {
		return err
	}
	if limit < 0 {
		return fmt.Errorf("%w: series limit must be >= 0", domain.ErrInvalid)
	}
	res, err := s.db.Writer().ExecContext(ctx, `UPDATE tenants SET series_limit = ? WHERE id = ?`,
		limit, encID(domain.ID(t)))
	if err != nil {
		return fmt.Errorf("set series limit: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("tenant: %w", domain.ErrNotFound)
	}
	return nil
}

// SetLogLimit implements identity.TenantRepo.
func (s *Store) SetLogLimit(ctx context.Context, ops domain.OpsScope, t domain.TenantID, bytes int64) error {
	if _, err := ops.Reason(); err != nil {
		return err
	}
	if bytes < 0 {
		return fmt.Errorf("%w: log limit must be >= 0", domain.ErrInvalid)
	}
	res, err := s.db.Writer().ExecContext(ctx, `UPDATE tenants SET log_bytes_per_day = ? WHERE id = ?`,
		bytes, encID(domain.ID(t)))
	if err != nil {
		return fmt.Errorf("set log limit: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("tenant: %w", domain.ErrNotFound)
	}
	return nil
}
