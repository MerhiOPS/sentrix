package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// RawCore implements query.CoreReader: samples in [from, to), by time.
func (s *Store) RawCore(ctx context.Context, sc domain.Scope, host domain.HostID, from, to time.Time) ([]domain.CoreSample, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	hk, err := hostKey(ctx, r, tk, host)
	if err != nil {
		return nil, err
	}
	rows, err := r.QueryContext(ctx, `SELECT ts, cpu, mem, disk, load1, load5, load15,
		mem_used, uptime, net_rx, net_tx, disk_r, disk_w
		FROM metrics_core WHERE tenant_key = ? AND host_key = ? AND ts >= ? AND ts < ?
		ORDER BY ts`, tk, hk, encTime(from), encTime(to))
	if err != nil {
		return nil, fmt.Errorf("read core: %w", err)
	}
	defer rows.Close()
	var out []domain.CoreSample
	for rows.Next() {
		var ts, cpu, mem, disk, l1, l5, l15 int64
		var mu, up, rx, tx, dr, dw int64
		if err := rows.Scan(&ts, &cpu, &mem, &disk, &l1, &l5, &l15, &mu, &up, &rx, &tx, &dr, &dw); err != nil {
			return nil, fmt.Errorf("scan core: %w", err)
		}
		smp := domain.CoreSample{
			TS: decTime(ts), CPUPercent: decGauge(cpu), MemoryPercent: decGauge(mem), DiskPercent: decGauge(disk),
			Load1: decGauge(l1), Load5: decGauge(l5), Load15: decGauge(l15),
		}
		if err := decInto([]int64{mu, up, rx, tx, dr, dw}, []*uint64{&smp.MemoryUsed, &smp.UptimeSeconds,
			&smp.NetRxBytes, &smp.NetTxBytes, &smp.DiskReadBytes, &smp.DiskWriteBytes}); err != nil {
			return nil, err
		}
		out = append(out, smp)
	}
	return out, rows.Err()
}
