package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/retention"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// pruneSQL deletes one bounded batch of a kind for one (tenant, host), oldest
// first, walking the primary-key prefix. Arguments: tenant, host, cutoff,
// limit (each statement binds them in that order). Deleting logs rows fires
// the trigger that removes their FTS entries.
var pruneSQL = map[domain.DataKind]string{
	domain.DataRawCore: `DELETE FROM metrics_core WHERE tenant_key = ?1 AND host_key = ?2 AND ts IN
		(SELECT ts FROM metrics_core WHERE tenant_key = ?1 AND host_key = ?2 AND ts < ?3 ORDER BY ts LIMIT ?4)`,
	domain.DataProcesses: `DELETE FROM process_snapshots WHERE tenant_key = ?1 AND host_key = ?2 AND ts IN
		(SELECT ts FROM process_snapshots WHERE tenant_key = ?1 AND host_key = ?2 AND ts < ?3 ORDER BY ts LIMIT ?4)`,
	domain.DataRollup1m: `DELETE FROM rollup_core_1m WHERE tenant_key = ?1 AND host_key = ?2 AND bucket IN
		(SELECT bucket FROM rollup_core_1m WHERE tenant_key = ?1 AND host_key = ?2 AND bucket < ?3 ORDER BY bucket LIMIT ?4)`,
	domain.DataRollup1h: `DELETE FROM rollup_core_1h WHERE tenant_key = ?1 AND host_key = ?2 AND bucket IN
		(SELECT bucket FROM rollup_core_1h WHERE tenant_key = ?1 AND host_key = ?2 AND bucket < ?3 ORDER BY bucket LIMIT ?4)`,
	domain.DataDetail: `DELETE FROM samples_detail WHERE tenant_key = ?1 AND (series_key, ts) IN
		(SELECT d.series_key, d.ts FROM samples_detail d JOIN series s ON s.tenant_key = d.tenant_key AND s.key = d.series_key
		 WHERE d.tenant_key = ?1 AND s.host_key = ?2 AND d.ts < ?3 LIMIT ?4)`,
	domain.DataLogs: `DELETE FROM logs WHERE tenant_key = ?1 AND id IN
		(SELECT id FROM logs WHERE tenant_key = ?1 AND host_key = ?2 AND ts < ?3 ORDER BY ts LIMIT ?4)`,
}

// oldestSQL finds the oldest row of a kind for one (tenant, host).
var oldestSQL = map[domain.DataKind]string{
	domain.DataRawCore:   `SELECT MIN(ts) FROM metrics_core WHERE tenant_key = ?1 AND host_key = ?2`,
	domain.DataProcesses: `SELECT MIN(ts) FROM process_snapshots WHERE tenant_key = ?1 AND host_key = ?2`,
	domain.DataRollup1m:  `SELECT MIN(bucket) FROM rollup_core_1m WHERE tenant_key = ?1 AND host_key = ?2`,
	domain.DataRollup1h:  `SELECT MIN(bucket) FROM rollup_core_1h WHERE tenant_key = ?1 AND host_key = ?2`,
	domain.DataDetail: `SELECT MIN(d.ts) FROM samples_detail d JOIN series s ON s.tenant_key = d.tenant_key AND s.key = d.series_key
		WHERE d.tenant_key = ?1 AND s.host_key = ?2`,
	domain.DataLogs: `SELECT MIN(ts) FROM logs WHERE tenant_key = ?1 AND host_key = ?2`,
}

// RetentionTargets implements retention.Store: every host of every tenant.
// It is one of the deliberately unscoped reads (it runs under an OpsScope).
func (s *Store) RetentionTargets(ctx context.Context, ops domain.OpsScope) ([]retention.Target, error) {
	if _, err := ops.Reason(); err != nil {
		return nil, err
	}
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT t.id, h.id FROM hosts h JOIN tenants t ON t.key = h.tenant_key ORDER BY h.key`)
	if err != nil {
		return nil, fmt.Errorf("list retention targets: %w", err)
	}
	defer rows.Close()
	var out []retention.Target
	for rows.Next() {
		var rawT, rawH []byte
		if err := rows.Scan(&rawT, &rawH); err != nil {
			return nil, fmt.Errorf("scan retention target: %w", err)
		}
		tid, err := decID(rawT)
		if err != nil {
			return nil, err
		}
		hid, err := decID(rawH)
		if err != nil {
			return nil, err
		}
		out = append(out, retention.Target{Tenant: domain.TenantID(tid), Host: domain.HostID(hid)})
	}
	return out, rows.Err()
}

// PruneHost implements retention.Store.
func (s *Store) PruneHost(ctx context.Context, ops domain.OpsScope, t retention.Target, k domain.DataKind,
	before time.Time, limit int) (int, error) {
	stmt, ok := pruneSQL[k]
	if !ok {
		return 0, fmt.Errorf("%w: data kind %q", domain.ErrInvalid, k)
	}
	w := s.db.Writer()
	tk, hk, err := targetKeys(ctx, w, ops, t)
	if err != nil {
		return 0, err
	}
	res, err := w.ExecContext(ctx, stmt, tk, hk, encTime(before), limit)
	if err != nil {
		return 0, fmt.Errorf("prune %s: %w", k, err)
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// OldestHost implements retention.Store.
func (s *Store) OldestHost(ctx context.Context, ops domain.OpsScope, t retention.Target, k domain.DataKind) (time.Time, bool, error) {
	stmt, ok := oldestSQL[k]
	if !ok {
		return time.Time{}, false, fmt.Errorf("%w: data kind %q", domain.ErrInvalid, k)
	}
	r := s.db.Reader()
	tk, hk, err := targetKeys(ctx, r, ops, t)
	if err != nil {
		return time.Time{}, false, err
	}
	var oldest sql.NullInt64
	if err := r.QueryRowContext(ctx, stmt, tk, hk).Scan(&oldest); err != nil {
		return time.Time{}, false, fmt.Errorf("oldest %s: %w", k, err)
	}
	return decTime(oldest.Int64), oldest.Valid, nil
}

// Compact implements retention.Store: return freed pages to the filesystem
// (auto_vacuum is incremental) and do a bounded amount of FTS segment
// merging — never a full optimize, which would hold the writer for as long
// as the index is large.
func (s *Store) Compact(ctx context.Context, ops domain.OpsScope) error {
	if _, err := ops.Reason(); err != nil {
		return err
	}
	w := s.db.Writer()
	if _, err := w.ExecContext(ctx, `PRAGMA incremental_vacuum`); err != nil {
		return fmt.Errorf("incremental vacuum: %w", err)
	}
	if _, err := w.ExecContext(ctx, `INSERT INTO logs_fts (logs_fts, rank) VALUES ('merge', 500)`); err != nil {
		return fmt.Errorf("merge fts segments: %w", err)
	}
	return nil
}

func targetKeys(ctx context.Context, q rowQuerier, ops domain.OpsScope, t retention.Target) (int64, int64, error) {
	if _, err := ops.Reason(); err != nil {
		return 0, 0, err
	}
	sc, err := domain.NewScope(t.Tenant)
	if err != nil {
		return 0, 0, err
	}
	tk, err := tenantKey(ctx, q, sc)
	if err != nil {
		return 0, 0, err
	}
	hk, err := hostKey(ctx, q, tk, t.Host)
	return tk, hk, err
}
