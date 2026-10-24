package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// utcDay is days since the Unix epoch: the log quota's accounting period.
func utcDay(t time.Time) int64 { return t.UTC().Unix() / 86400 }

// logBudget is the tenant's bytes left today, read in the batch transaction
// so two concurrent batches cannot both spend the same remainder.
func logBudget(ctx context.Context, tx *batchTx, tk, day int64) (int64, error) {
	var limit, used int64
	if err := tx.QueryRowContext(ctx, `SELECT log_bytes_per_day FROM tenants WHERE key = ?`, tk).Scan(&limit); err != nil {
		return 0, fmt.Errorf("log quota: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(sum(bytes), 0) FROM log_usage WHERE tenant_key = ? AND day = ?`,
		tk, day).Scan(&used); err != nil {
		return 0, fmt.Errorf("log usage: %w", err)
	}
	return limit - used, nil
}

// writeLogs stores redacted lines; the FTS index follows by trigger.
func writeLogs(ctx context.Context, tx *batchTx, tk, hk int64, lines []domain.LogLine) error {
	for _, l := range lines {
		labels := l.Labels
		if labels == nil {
			labels = domain.Labels{}
		}
		lb, err := json.Marshal(labels)
		if err != nil {
			return fmt.Errorf("encode log labels: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO logs (tenant_key, host_key, ts, source, level, message, labels)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, tk, hk, encTime(l.TS), l.Source, string(l.Level), l.Message, string(lb)); err != nil {
			return fmt.Errorf("insert log: %w", err)
		}
	}
	return nil
}

// recordLogUsage meters the batch — even when no line was stored, so lines
// dropped, shed or redacted away are always attributable (OLD C-M11).
func recordLogUsage(ctx context.Context, tx *batchTx, tk, hk, day int64, p ingest.Prepared) error {
	st := p.LogStats
	if st.Received == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO log_usage
		(tenant_key, host_key, day, bytes, stored, received, redacted, dropped, shed) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (tenant_key, host_key, day) DO UPDATE SET
			bytes = bytes + excluded.bytes, stored = stored + excluded.stored, received = received + excluded.received,
			redacted = redacted + excluded.redacted, dropped = dropped + excluded.dropped, shed = shed + excluded.shed`,
		tk, hk, day, st.Bytes, len(p.Logs), st.Received, st.Redacted, st.Dropped, st.Shed)
	if err != nil {
		return fmt.Errorf("record log usage: %w", err)
	}
	return nil
}
