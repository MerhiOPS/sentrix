package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// rollupGauges and rollupCounters are the rollup columns, in table order.
var (
	rollupGauges   = []string{"cpu", "mem", "disk", "load1", "load5", "load15", "mem_used"}
	rollupCounters = []string{"net_rx", "net_tx", "disk_r", "disk_w"}
)

// rollupTable is one resolution's table.
type rollupTable struct {
	name   string
	width  time.Duration
	upsert string
}

var rollupTables = []rollupTable{
	{name: "rollup_core_1m", width: time.Minute},
	{name: "rollup_core_1h", width: time.Hour},
}

func init() {
	for i := range rollupTables {
		rollupTables[i].upsert = buildRollupUpsert(rollupTables[i].name)
	}
}

// buildRollupUpsert generates the per-sample upsert. In SQLite's UPDATE SET
// every expression sees the *old* row, so the CASEs compare the incoming
// timestamp with the stored first_ts/last_ts before either moves.
func buildRollupUpsert(table string) string {
	cols := []string{"tenant_key", "host_key", "bucket", "n"}
	var sets []string
	sets = append(sets, "n = n + 1")
	for _, g := range rollupGauges {
		cols = append(cols, g+"_sum", g+"_min", g+"_max")
		sets = append(sets,
			fmt.Sprintf("%[1]s_sum = %[1]s_sum + excluded.%[1]s_sum", g),
			fmt.Sprintf("%[1]s_min = min(%[1]s_min, excluded.%[1]s_min)", g),
			fmt.Sprintf("%[1]s_max = max(%[1]s_max, excluded.%[1]s_max)", g))
	}
	for _, c := range rollupCounters {
		cols = append(cols, c+"_first", c+"_last")
		sets = append(sets,
			fmt.Sprintf("%[1]s_first = CASE WHEN excluded.first_ts < first_ts THEN excluded.%[1]s_first ELSE %[1]s_first END", c),
			fmt.Sprintf("%[1]s_last = CASE WHEN excluded.last_ts > last_ts THEN excluded.%[1]s_last ELSE %[1]s_last END", c))
	}
	cols = append(cols, "first_ts", "last_ts")
	sets = append(sets, "first_ts = min(first_ts, excluded.first_ts)", "last_ts = max(last_ts, excluded.last_ts)")
	return "INSERT INTO " + table + " (" + strings.Join(cols, ", ") + ") VALUES (" +
		strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ") + ")\n" +
		"ON CONFLICT (tenant_key, host_key, bucket) DO UPDATE SET " + strings.Join(sets, ", ")
}

// rollupSample folds one inserted sample into every resolution.
func rollupSample(ctx context.Context, tx *sql.Tx, tk, hk int64, s domain.CoreSample) error {
	gauges := []int64{encGauge(s.CPUPercent), encGauge(s.MemoryPercent), encGauge(s.DiskPercent),
		encGauge(s.Load1), encGauge(s.Load5), encGauge(s.Load15)}
	memUsed, err := encU64(s.MemoryUsed)
	if err != nil {
		return err
	}
	gauges = append(gauges, memUsed)
	var counters []int64
	for _, v := range []uint64{s.NetRxBytes, s.NetTxBytes, s.DiskReadBytes, s.DiskWriteBytes} {
		c, err := encU64(v)
		if err != nil {
			return err
		}
		counters = append(counters, c)
	}
	ts := encTime(s.TS)
	for _, rt := range rollupTables {
		args := []any{tk, hk, s.TS.Truncate(rt.width).UnixMilli(), 1}
		for _, g := range gauges {
			args = append(args, g, g, g)
		}
		for _, c := range counters {
			args = append(args, c, c)
		}
		args = append(args, ts, ts)
		if _, err := tx.ExecContext(ctx, rt.upsert, args...); err != nil {
			return fmt.Errorf("rollup %s: %w", rt.name, err)
		}
	}
	return nil
}
