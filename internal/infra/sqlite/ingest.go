package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// CommitBatch implements ingest.Store. One BEGIN IMMEDIATE transaction:
// replay check → counter state → build → rows → latest → counters → host →
// seq record. Anything failing rolls all of it back (R1 by construction).
func (s *Store) CommitBatch(ctx context.Context, sc domain.Scope, host domain.HostID, seq uint64,
	build ingest.BuildFunc) (ingest.CommitResult, error) {
	tx, err := s.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return ingest.CommitResult{}, fmt.Errorf("begin batch: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	tk, err := tenantKey(ctx, tx, sc)
	if err != nil {
		return ingest.CommitResult{}, err
	}
	hk, err := hostKey(ctx, tx, tk, host)
	if err != nil {
		return ingest.CommitResult{}, err
	}
	seqVal, err := encU64(seq)
	if err != nil {
		return ingest.CommitResult{}, err
	}
	if replayed, err := seenSeq(ctx, tx, tk, hk, seqVal); err != nil || replayed {
		return ingest.CommitResult{Replayed: replayed}, err
	}
	state, err := loadCounters(ctx, tx, tk, hk)
	if err != nil {
		return ingest.CommitResult{}, err
	}
	p, err := build(state)
	if err != nil {
		return ingest.CommitResult{}, fmt.Errorf("build batch: %w", err)
	}
	sr := &seriesResolver{tx: tx, tk: tk, hk: hk, cache: &s.series, pending: map[seriesID]int64{}}
	res, err := writePrepared(ctx, tx, tk, hk, p, sr)
	if err != nil {
		return ingest.CommitResult{}, err
	}
	if err := recordSeq(ctx, tx, tk, hk, seqVal, p); err != nil {
		return ingest.CommitResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ingest.CommitResult{}, fmt.Errorf("commit batch: %w", err)
	}
	s.series.merge(sr.pending) // only now do new series exist for other batches
	return res, nil
}

func seenSeq(ctx context.Context, tx *sql.Tx, tk, hk, seq int64) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM ingest_batches WHERE tenant_key = ? AND host_key = ? AND seq = ?`,
		tk, hk, seq).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("replay check: %w", err)
	}
	return n > 0, nil
}

// recordSeq stores the replay record and prunes this host's records older
// than p.SeqRetention — cheap per batch, bounded per host.
func recordSeq(ctx context.Context, tx *sql.Tx, tk, hk, seq int64, p ingest.Prepared) error {
	now := encTime(p.ReceivedAt)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO ingest_batches (tenant_key, host_key, seq, received_at) VALUES (?, ?, ?, ?)`,
		tk, hk, seq, now); err != nil {
		return fmt.Errorf("record seq: %w", err)
	}
	if p.SeqRetention <= 0 {
		return fmt.Errorf("prepared batch has no seq retention")
	}
	cutoff := now - p.SeqRetention.Milliseconds()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM ingest_batches WHERE tenant_key = ? AND host_key = ? AND received_at < ?`,
		tk, hk, cutoff); err != nil {
		return fmt.Errorf("prune seqs: %w", err)
	}
	return nil
}
