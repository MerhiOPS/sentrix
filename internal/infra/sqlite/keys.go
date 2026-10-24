package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// rowQuerier is satisfied by *sql.DB, *sql.Conn and *sql.Tx.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// tenantKey resolves a scope to its internal key. Every scoped adapter
// method starts here, so an unbuilt Scope fails before any SQL runs.
func tenantKey(ctx context.Context, q rowQuerier, s domain.Scope) (int64, error) {
	t, err := s.Tenant()
	if err != nil {
		return 0, err
	}
	var key int64
	err = q.QueryRowContext(ctx, `SELECT key FROM tenants WHERE id = ?`, encID(domain.ID(t))).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("tenant: %w", domain.ErrNotFound)
	}
	if err != nil {
		return 0, fmt.Errorf("resolve tenant: %w", err)
	}
	return key, nil
}

// hostKey resolves a host within a tenant. Binding tenant_key is the
// isolation: another tenant's host id is simply not found.
func hostKey(ctx context.Context, q rowQuerier, tenantKey int64, h domain.HostID) (int64, error) {
	var key int64
	err := q.QueryRowContext(ctx,
		`SELECT key FROM hosts WHERE tenant_key = ? AND id = ?`, tenantKey, encID(domain.ID(h))).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("host: %w", domain.ErrNotFound)
	}
	if err != nil {
		return 0, fmt.Errorf("resolve host: %w", err)
	}
	return key, nil
}
