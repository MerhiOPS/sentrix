package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// seriesID identifies a series within the adapter's keys.
type seriesID struct {
	host   int64
	metric string
	hash   string
}

// seriesCache maps series to their keys. Entries are added only after the
// transaction that created them commits (R1): a cached key for a rolled-back
// insert would point at a row that does not exist.
type seriesCache struct {
	mu   sync.RWMutex
	keys map[seriesID]int64
}

func (c *seriesCache) get(id seriesID) (int64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	k, ok := c.keys[id]
	return k, ok
}

func (c *seriesCache) merge(pending map[seriesID]int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.keys == nil {
		c.keys = map[seriesID]int64{}
	}
	for id, k := range pending {
		c.keys[id] = k
	}
}

// seriesResolver resolves series within one batch transaction.
type seriesResolver struct {
	tx      *sql.Tx
	tk, hk  int64
	cache   *seriesCache
	pending map[seriesID]int64
	room    int  // series the tenant may still create; loaded lazily
	loaded  bool // whether room has been loaded
}

// resolve returns the series key, creating the series if the tenant's
// ceiling allows. ok=false means "refused by the ceiling": the caller drops
// and counts the sample; the batch goes on (OLD A-C1).
func (r *seriesResolver) resolve(ctx context.Context, d domain.DetailSample, m domain.DetailMetric) (int64, bool, error) {
	k := domain.KeyFor(d.Metric, d.Labels)
	id := seriesID{host: r.hk, metric: k.Metric, hash: k.LabelsHash}
	if key, ok := r.pending[id]; ok {
		return key, true, nil
	}
	if key, ok := r.cache.get(id); ok {
		return key, true, nil
	}
	var key int64
	err := r.tx.QueryRowContext(ctx, `SELECT key FROM series
		WHERE tenant_key = ? AND host_key = ? AND metric = ? AND labels_hash = ?`,
		r.tk, r.hk, k.Metric, k.LabelsHash).Scan(&key)
	if err == nil {
		r.pending[id] = key // known to exist: safe to cache after commit too
		return key, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, fmt.Errorf("find series: %w", err)
	}
	if err := r.loadRoom(ctx); err != nil {
		return 0, false, err
	}
	if r.room <= 0 {
		return 0, false, nil
	}
	labels, err := json.Marshal(d.Labels) // encoding/json sorts map keys: deterministic
	if err != nil {
		return 0, false, fmt.Errorf("encode labels: %w", err)
	}
	res, err := r.tx.ExecContext(ctx, `INSERT INTO series
		(tenant_key, host_key, metric, labels_hash, labels, kind, scale, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.tk, r.hk, k.Metric, k.LabelsHash, string(labels), string(m.Kind), m.Scale, encTime(d.TS))
	if err != nil {
		return 0, false, fmt.Errorf("create series: %w", err)
	}
	if key, err = res.LastInsertId(); err != nil {
		return 0, false, err
	}
	r.room--
	r.pending[id] = key
	return key, true, nil
}

// loadRoom reads the tenant's ceiling and current series count once per batch.
func (r *seriesResolver) loadRoom(ctx context.Context) error {
	if r.loaded {
		return nil
	}
	var limit, count int
	if err := r.tx.QueryRowContext(ctx, `SELECT series_limit FROM tenants WHERE key = ?`, r.tk).Scan(&limit); err != nil {
		return fmt.Errorf("series limit: %w", err)
	}
	if err := r.tx.QueryRowContext(ctx, `SELECT count(*) FROM series WHERE tenant_key = ?`, r.tk).Scan(&count); err != nil {
		return fmt.Errorf("series count: %w", err)
	}
	r.room, r.loaded = limit-count, true
	return nil
}
