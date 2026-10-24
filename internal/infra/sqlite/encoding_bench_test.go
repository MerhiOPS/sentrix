package sqlite

// Encoding benchmark (PLAN Phase 1): decide how core gauges are stored
// before any data exists. SQLite has no column compression; a REAL is always
// 8 bytes in a record, while an INTEGER takes 1–4 bytes at these magnitudes.
// So "percent × 100 as INTEGER" is the SQLite analogue of OLD's
// Float32-rounded-to-2dp saving. Measured, not assumed:
//
//	go test ./internal/infra/sqlite -run '^$' -bench Encoding -benchtime 1x

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

const (
	benchHosts   = 20
	benchSamples = 8640 // 12 h at 5 s
	benchBatch   = 2    // samples per host per 10 s batch
)

func coreTableDDL(key, gauge string) string {
	return fmt.Sprintf(`CREATE TABLE core (
		tenant_id %[2]s NOT NULL, host_id %[2]s NOT NULL, ts INTEGER NOT NULL,
		cpu %[1]s, mem %[1]s, disk %[1]s, load1 %[1]s, load5 %[1]s, load15 %[1]s,
		mem_used INTEGER, uptime INTEGER,
		net_rx INTEGER, net_tx INTEGER, disk_r INTEGER, disk_w INTEGER,
		PRIMARY KEY (tenant_id, host_id, ts)
	) WITHOUT ROWID, STRICT`, gauge, key)
}

// gaugeValue is a smooth, autocorrelated series like real telemetry:
// benchmarking against noise would understate any encoding's advantage.
func gaugeValue(host, i, phase int) float64 {
	v := 40 + 30*math.Sin(float64(i)/180+float64(host+phase)) + float64((i*7+host)%13)/10
	return math.Round(v*100) / 100
}

func BenchmarkEncoding(b *testing.B) {
	asReal := func(v float64) any { return v }
	asInt := func(v float64) any { return int64(math.Round(v * 100)) }
	for _, enc := range []struct {
		name    string
		ddl     string
		gauge   func(float64) any
		intKeys bool
	}{
		{"real_uuidkeys", coreTableDDL("BLOB", "REAL"), asReal, false},
		{"int_x100_uuidkeys", coreTableDDL("BLOB", "INTEGER"), asInt, false},
		{"int_x100_intkeys", coreTableDDL("INTEGER", "INTEGER"), asInt, true},
	} {
		b.Run(enc.name, func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				path := filepath.Join(b.TempDir(), "enc.db")
				db, err := Open(context.Background(), path)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := db.Writer().Exec(enc.ddl); err != nil {
					b.Fatal(err)
				}
				b.ResetTimer()
				if err := loadCore(db.Writer(), enc.gauge, enc.intKeys); err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				reportSize(b, db, path)
				if err := db.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func loadCore(w *sql.DB, gauge func(float64) any, intKeys bool) error {
	for i := 0; i < benchSamples; i += benchBatch {
		for h := 0; h < benchHosts; h++ {
			tenant, host := benchKeys(h, intKeys)
			if err := writeBatch(w, tenant, host, h, i, gauge); err != nil {
				return err
			}
		}
	}
	return nil
}

// benchKeys returns realistic keys: random-looking 16-byte UUIDs, or small
// adapter-internal integers.
func benchKeys(h int, intKeys bool) (tenant, host any) {
	if intKeys {
		return int64(1), int64(h + 1)
	}
	t, hb := make([]byte, 16), make([]byte, 16)
	for k := range t {
		t[k] = byte(0x9e ^ k*31)
		hb[k] = byte(h*17 + k*53 + 7)
	}
	return t, hb
}

// writeBatch mirrors ingest: one transaction per host batch.
func writeBatch(w *sql.DB, tenant, host any, h, i int, gauge func(float64) any) error {
	tx, err := w.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for j := i; j < i+benchBatch; j++ {
		ts := int64(1_790_000_000_000 + j*5000)
		_, err := tx.Exec(`INSERT INTO core VALUES (?,?,?, ?,?,?,?,?,?, ?,?, ?,?,?,?)`,
			tenant, host, ts,
			gauge(gaugeValue(h, j, 0)), gauge(gaugeValue(h, j, 1)), gauge(gaugeValue(h, j, 2)),
			gauge(gaugeValue(h, j, 3)/40), gauge(gaugeValue(h, j, 4)/40), gauge(gaugeValue(h, j, 5)/40),
			int64(8<<30)+int64(j)*4096, int64(j*5),
			int64(j)*125_000, int64(j)*31_000, int64(j)*52_000, int64(j)*91_000)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func reportSize(b *testing.B, db *DB, path string) {
	if _, err := db.Writer().Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		b.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		b.Fatal(err)
	}
	rows := float64(benchHosts * benchSamples)
	b.ReportMetric(float64(fi.Size())/rows, "B/sample")
	b.ReportMetric(float64(fi.Size())/(rows*12), "B/value")
	b.ReportMetric(rows/b.Elapsed().Seconds(), "samples/s")
	b.ReportMetric(rows/benchBatch/b.Elapsed().Seconds(), "batches/s")
}
