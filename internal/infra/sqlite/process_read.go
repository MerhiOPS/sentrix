package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// ProcessesAt implements query.ProcessReader.
func (s *Store) ProcessesAt(ctx context.Context, sc domain.Scope, host domain.HostID, at time.Time) (domain.ProcessSnapshot, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return domain.ProcessSnapshot{}, err
	}
	hk, err := hostKey(ctx, r, tk, host)
	if err != nil {
		return domain.ProcessSnapshot{}, err
	}
	var ts int64
	var blob string
	err = r.QueryRowContext(ctx, `SELECT ts, procs FROM process_snapshots
		WHERE tenant_key = ? AND host_key = ? AND ts <= ? ORDER BY ts DESC LIMIT 1`,
		tk, hk, encTime(at)).Scan(&ts, &blob)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ProcessSnapshot{}, fmt.Errorf("process snapshot: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.ProcessSnapshot{}, fmt.Errorf("read snapshot: %w", err)
	}
	var procs []procJSON
	if err := json.Unmarshal([]byte(blob), &procs); err != nil {
		return domain.ProcessSnapshot{}, fmt.Errorf("decode snapshot: %w", err)
	}
	snap := domain.ProcessSnapshot{TS: decTime(ts), Procs: make([]domain.Process, len(procs))}
	for i, p := range procs {
		snap.Procs[i] = domain.Process{Name: p.Name, CPUPercent: p.CPU, RSS: p.RSS}
	}
	return snap, nil
}
