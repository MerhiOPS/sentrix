package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// ListSeries implements query.DetailReader, ordered by metric then labels.
func (s *Store) ListSeries(ctx context.Context, sc domain.Scope, host domain.HostID) ([]domain.Series, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	hk, err := hostKey(ctx, r, tk, host)
	if err != nil {
		return nil, err
	}
	rows, err := r.QueryContext(ctx, `SELECT key, metric, labels_hash, labels, kind FROM series
		WHERE tenant_key = ? AND host_key = ? ORDER BY metric, labels`, tk, hk)
	if err != nil {
		return nil, fmt.Errorf("list series: %w", err)
	}
	defer rows.Close()
	var out []domain.Series
	for rows.Next() {
		var sr domain.Series
		var labels, kind string
		if err := rows.Scan(&sr.ID, &sr.Key.Metric, &sr.Key.LabelsHash, &labels, &kind); err != nil {
			return nil, fmt.Errorf("scan series: %w", err)
		}
		if sr.Labels, err = decodeLabels(labels); err != nil {
			return nil, err
		}
		sr.Host, sr.Kind = host, domain.MetricKind(kind)
		out = append(out, sr)
	}
	return out, rows.Err()
}

// decodeLabels reads the stored JSON label object.
func decodeLabels(raw string) (domain.Labels, error) {
	l := domain.Labels{}
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		return nil, fmt.Errorf("decode series labels: %w", err)
	}
	return l, nil
}

// DetailPoints implements query.DetailReader. The series must belong to
// this host of this tenant, or it is not found.
func (s *Store) DetailPoints(ctx context.Context, sc domain.Scope, host domain.HostID, series int64, from, to time.Time) ([]query.DetailPoint, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	hk, err := hostKey(ctx, r, tk, host)
	if err != nil {
		return nil, err
	}
	var scale int64
	err = r.QueryRowContext(ctx, `SELECT scale FROM series WHERE tenant_key = ? AND host_key = ? AND key = ?`,
		tk, hk, series).Scan(&scale)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("series: %w", domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve series: %w", err)
	}
	rows, err := r.QueryContext(ctx, `SELECT ts, value FROM samples_detail
		WHERE tenant_key = ? AND series_key = ? AND ts >= ? AND ts < ? ORDER BY ts`,
		tk, series, encTime(from), encTime(to))
	if err != nil {
		return nil, fmt.Errorf("read detail: %w", err)
	}
	defer rows.Close()
	var out []query.DetailPoint
	for rows.Next() {
		var ts, v int64
		if err := rows.Scan(&ts, &v); err != nil {
			return nil, fmt.Errorf("scan detail: %w", err)
		}
		out = append(out, query.DetailPoint{TS: decTime(ts), Value: float64(v) / float64(scale)})
	}
	return out, rows.Err()
}
