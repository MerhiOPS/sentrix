package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"

	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// writeDetails stores detail samples, creating series within the ceiling.
func writeDetails(ctx context.Context, r *seriesResolver, details []domain.DetailSample, res *ingest.CommitResult) error {
	for _, d := range details {
		m, ok := domain.LookupMetric(d.Metric)
		if !ok {
			return fmt.Errorf("detail metric %q passed validation but is not in the catalog", d.Metric)
		}
		key, ok, err := r.resolve(ctx, d, m)
		if err != nil {
			return err
		}
		if !ok {
			res.SeriesRefused++
			continue
		}
		v := int64(math.Round(d.Value * float64(m.Scale)))
		out, err := r.tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO samples_detail (tenant_key, series_key, ts, value) VALUES (?, ?, ?, ?)`,
			r.tk, key, encTime(d.TS), v)
		if err != nil {
			return fmt.Errorf("insert detail: %w", err)
		}
		if n, err := out.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			res.Conflicts++
		}
	}
	return nil
}

type procJSON struct {
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"`
	RSS  uint64  `json:"rss"`
}

// writeSnapshots stores process snapshots as JSON blobs.
func writeSnapshots(ctx context.Context, tx *sql.Tx, tk, hk int64, snaps []domain.ProcessSnapshot, res *ingest.CommitResult) error {
	for _, s := range snaps {
		procs := make([]procJSON, len(s.Procs))
		for i, p := range s.Procs {
			procs[i] = procJSON{Name: p.Name, CPU: math.Round(p.CPUPercent*100) / 100, RSS: p.RSS}
		}
		blob, err := json.Marshal(procs)
		if err != nil {
			return fmt.Errorf("encode snapshot: %w", err)
		}
		out, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO process_snapshots (tenant_key, host_key, ts, procs) VALUES (?, ?, ?, ?)`,
			tk, hk, encTime(s.TS), string(blob))
		if err != nil {
			return fmt.Errorf("insert snapshot: %w", err)
		}
		if n, err := out.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			res.Conflicts++
		}
	}
	return nil
}
