package sqlite

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

var tenantBucketSelect = "r." + strings.ReplaceAll(rollupSelect, ", ", ", r.")

// TenantBuckets implements alerting.BucketReader: the 1m bucket starting at
// start, for every host of the tenant, in one indexed range read.
func (s *Store) TenantBuckets(ctx context.Context, sc domain.Scope, start time.Time) (map[domain.HostID]query.RollupBucket, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	//nolint:gosec // tenantBucketSelect is built from fixed column names at init, never from input
	rows, err := r.QueryContext(ctx, `SELECT h.id, `+tenantBucketSelect+` FROM rollup_core_1m r
		JOIN hosts h ON h.tenant_key = r.tenant_key AND h.key = r.host_key
		WHERE r.tenant_key = ? AND r.bucket = ?`, tk, encTime(start))
	if err != nil {
		return nil, fmt.Errorf("read tenant buckets: %w", err)
	}
	defer rows.Close()
	out := map[domain.HostID]query.RollupBucket{}
	var raw []byte
	for rows.Next() {
		b, err := scanRollup(rows.Scan, &raw)
		if err != nil {
			return nil, err
		}
		id, err := decID(raw)
		if err != nil {
			return nil, err
		}
		out[domain.HostID(id)] = b
	}
	return out, rows.Err()
}

// ListTenants implements alerting.TenantLister.
func (s *Store) ListTenants(ctx context.Context, ops domain.OpsScope) ([]domain.TenantID, error) {
	if _, err := ops.Reason(); err != nil {
		return nil, err
	}
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT id FROM tenants ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()
	var out []domain.TenantID
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}
		id, err := decID(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, domain.TenantID(id))
	}
	return out, rows.Err()
}
