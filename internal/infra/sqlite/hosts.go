package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

const hostColumns = `id, name, os, platform, agent_version, interval_ms, created_at, last_seen, disabled_at,
	buffer_depth, dropped_total, clock_skew_ms, rejected_total, conflict_total`

// GetHost implements query.HostReader.
func (s *Store) GetHost(ctx context.Context, sc domain.Scope, id domain.HostID) (domain.Host, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return domain.Host{}, err
	}
	tid, _ := sc.Tenant()
	row := r.QueryRowContext(ctx, `SELECT `+hostColumns+` FROM hosts WHERE tenant_key = ? AND id = ?`,
		tk, encID(domain.ID(id)))
	h, err := scanHost(row.Scan, tid)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Host{}, fmt.Errorf("host: %w", domain.ErrNotFound)
	}
	return h, err
}

// ListHosts implements query.HostReader, ordered by name then id.
func (s *Store) ListHosts(ctx context.Context, sc domain.Scope) ([]domain.Host, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	tid, _ := sc.Tenant()
	rows, err := r.QueryContext(ctx, `SELECT `+hostColumns+` FROM hosts WHERE tenant_key = ? ORDER BY name, id`, tk)
	if err != nil {
		return nil, fmt.Errorf("list hosts: %w", err)
	}
	defer rows.Close()
	var out []domain.Host
	for rows.Next() {
		h, err := scanHost(rows.Scan, tid)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func scanHost(scan func(...any) error, tenant domain.TenantID) (domain.Host, error) {
	var (
		raw                 []byte
		h                   domain.Host
		intervalMS, created int64
		lastSeen, disabled  sql.NullInt64
		dropped, rejected   int64
		conflicts, skewMS   int64
	)
	if err := scan(&raw, &h.Name, &h.OS, &h.Platform, &h.AgentVersion, &intervalMS, &created, &lastSeen, &disabled,
		&h.Health.BufferDepth, &dropped, &skewMS, &rejected, &conflicts); err != nil {
		return domain.Host{}, err
	}
	if err := decInto([]int64{dropped, rejected, conflicts},
		[]*uint64{&h.Health.DroppedTotal, &h.Health.RejectedTotal, &h.Health.ConflictTotal}); err != nil {
		return domain.Host{}, err
	}
	h.Health.ClockSkew = time.Duration(skewMS) * time.Millisecond
	id, err := decID(raw)
	if err != nil {
		return domain.Host{}, err
	}
	h.ID, h.Tenant = domain.HostID(id), tenant
	h.Interval = time.Duration(intervalMS) * time.Millisecond
	h.CreatedAt = decTime(created)
	if lastSeen.Valid {
		h.LastSeen = decTime(lastSeen.Int64)
	}
	if disabled.Valid {
		d := decTime(disabled.Int64)
		h.DisabledAt = &d
	}
	return h, nil
}
