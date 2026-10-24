package sqlite

import (
	"context"
	"fmt"
)

// Ready reports whether the database serves reads and the writer is
// draining: a write transaction holding the single writer past ctx's
// deadline makes the probe wait on the writer and fail.
func (s *Store) Ready(ctx context.Context) error {
	var one int
	if err := s.db.Reader().QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil {
		return fmt.Errorf("reader: %w", err)
	}
	if err := s.db.Writer().QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil {
		return fmt.Errorf("writer: %w", err)
	}
	return nil
}

// WriterWaits returns how many operations have waited for the writer and
// for how long in total, since start.
func (s *Store) WriterWaits() (int64, int64) {
	st := s.db.Writer().Stats()
	return st.WaitCount, st.WaitDuration.Milliseconds()
}
