package sqlite

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

var rollupSelect = func() string {
	cols := []string{"bucket", "n"}
	for _, g := range rollupGauges {
		cols = append(cols, g+"_sum", g+"_min", g+"_max")
	}
	for _, c := range rollupCounters {
		cols = append(cols, c+"_first", c+"_last")
	}
	return strings.Join(append(cols, "first_ts", "last_ts"), ", ")
}()

func rollupTableFor(res query.Resolution) (string, error) {
	switch res {
	case query.Res1m:
		return "rollup_core_1m", nil
	case query.Res1h:
		return "rollup_core_1h", nil
	default:
		return "", fmt.Errorf("%w: resolution %q", domain.ErrInvalid, res)
	}
}

// Rollups implements query.RollupReader.
func (s *Store) Rollups(ctx context.Context, sc domain.Scope, host domain.HostID, res query.Resolution, from, to time.Time) ([]query.RollupBucket, error) {
	table, err := rollupTableFor(res)
	if err != nil {
		return nil, err
	}
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	hk, err := hostKey(ctx, r, tk, host)
	if err != nil {
		return nil, err
	}
	//nolint:gosec // table comes from rollupTableFor's fixed whitelist, never from input
	rows, err := r.QueryContext(ctx, `SELECT `+rollupSelect+` FROM `+table+`
		WHERE tenant_key = ? AND host_key = ? AND bucket >= ? AND bucket < ? ORDER BY bucket`,
		tk, hk, encTime(from), encTime(to))
	if err != nil {
		return nil, fmt.Errorf("read rollups: %w", err)
	}
	defer rows.Close()
	var out []query.RollupBucket
	for rows.Next() {
		b, err := scanRollup(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func scanRollup(scan func(...any) error) (query.RollupBucket, error) {
	n := 2 + 3*len(rollupGauges) + 2*len(rollupCounters) + 2
	vals := make([]int64, n)
	ptrs := make([]any, n)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := scan(ptrs...); err != nil {
		return query.RollupBucket{}, fmt.Errorf("scan rollup: %w", err)
	}
	b := query.RollupBucket{Start: decTime(vals[0]), N: int(vals[1]), Gauges: map[query.Gauge]query.Agg{}, Counters: map[string]query.Edge{}}
	i := 2
	for _, g := range rollupGauges {
		dec := decGauge
		if g == "mem_used" {
			dec = func(v int64) float64 { return float64(v) }
		}
		b.Gauges[query.Gauge(g)] = query.Agg{Sum: dec(vals[i]), Min: dec(vals[i+1]), Max: dec(vals[i+2])}
		i += 3
	}
	for _, c := range rollupCounters {
		b.Counters[c] = query.Edge{First: float64(vals[i]), Last: float64(vals[i+1])}
		i += 2
	}
	b.FirstTS, b.LastTS = decTime(vals[i]), decTime(vals[i+1])
	return b, nil
}
