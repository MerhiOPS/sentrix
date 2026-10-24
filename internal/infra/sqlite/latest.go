package sqlite

import (
	"context"
	"fmt"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// LatestCore implements query.LatestReader.
func (s *Store) LatestCore(ctx context.Context, sc domain.Scope) (map[domain.HostID]domain.CoreSample, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	rows, err := r.QueryContext(ctx, `SELECT h.id, l.ts, l.cpu, l.mem, l.disk, l.load1, l.load5, l.load15,
		l.mem_used, l.uptime, l.net_rx, l.net_tx, l.disk_r, l.disk_w
		FROM latest_core l JOIN hosts h ON h.key = l.host_key AND h.tenant_key = l.tenant_key
		WHERE l.tenant_key = ?`, tk)
	if err != nil {
		return nil, fmt.Errorf("read latest: %w", err)
	}
	defer rows.Close()
	out := map[domain.HostID]domain.CoreSample{}
	for rows.Next() {
		var raw []byte
		var ts, cpu, mem, disk, l1, l5, l15, mu, up, rx, tx, dr, dw int64
		if err := rows.Scan(&raw, &ts, &cpu, &mem, &disk, &l1, &l5, &l15, &mu, &up, &rx, &tx, &dr, &dw); err != nil {
			return nil, fmt.Errorf("scan latest: %w", err)
		}
		id, err := decID(raw)
		if err != nil {
			return nil, err
		}
		smp := domain.CoreSample{TS: decTime(ts), CPUPercent: decGauge(cpu), MemoryPercent: decGauge(mem),
			DiskPercent: decGauge(disk), Load1: decGauge(l1), Load5: decGauge(l5), Load15: decGauge(l15)}
		if err := decInto([]int64{mu, up, rx, tx, dr, dw}, []*uint64{&smp.MemoryUsed, &smp.UptimeSeconds,
			&smp.NetRxBytes, &smp.NetTxBytes, &smp.DiskReadBytes, &smp.DiskWriteBytes}); err != nil {
			return nil, err
		}
		out[domain.HostID(id)] = smp
	}
	return out, rows.Err()
}
