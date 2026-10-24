package sqlite

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/storetest"
)

func seedDrillHosts(t *testing.T, st *Store, n int) []storetest.BenchHost {
	t.Helper()
	return storetest.SeedBenchHosts(t, st, n)
}

// drillBatch is a realistic 10 s batch: 2 samples, 36 detail values and 5
// log lines.
func drillBatch(at time.Time, host int) ingest.BuildFunc {
	return func(st ingest.TxState) (ingest.Prepared, error) {
		p := ingest.Prepared{Counters: st.Counters, Interval: 5 * time.Second, ReceivedAt: time.Now(), SeqRetention: 72 * time.Hour}
		for s := range 2 {
			ts := at.Add(time.Duration(s) * 5 * time.Second)
			p.Samples = append(p.Samples, domain.CoreSample{TS: ts, CPUPercent: 30 + float64(host), MemoryPercent: 55, NetRxBytes: uint64(ts.Unix())})
			for c := range 8 {
				p.Details = append(p.Details, domain.DetailSample{TS: ts, Metric: domain.MetricCPUCore, Kind: domain.KindGauge,
					Labels: domain.Labels{"core": fmt.Sprint(c)}, Value: 20})
			}
			for _, m := range []string{"/", "/var"} {
				p.Details = append(p.Details, domain.DetailSample{TS: ts, Metric: domain.MetricDiskUsed, Kind: domain.KindGauge,
					Labels: domain.Labels{"mount": m}, Value: 40})
			}
			for _, nic := range []string{"eth0", "eth1"} {
				for _, m := range []string{domain.MetricNetRx, domain.MetricNetTx} {
					p.Details = append(p.Details, domain.DetailSample{TS: ts, Metric: m, Kind: domain.KindCounter,
						Labels: domain.Labels{"nic": nic}, Value: float64(ts.Unix())})
				}
			}
			for _, dev := range []string{"sda", "sdb"} {
				for _, m := range []string{domain.MetricDiskRead, domain.MetricDiskWrite} {
					p.Details = append(p.Details, domain.DetailSample{TS: ts, Metric: m, Kind: domain.KindCounter,
						Labels: domain.Labels{"device": dev}, Value: float64(ts.Unix())})
				}
			}
		}
		for l := range 5 {
			p.Logs = append(p.Logs, domain.LogLine{TS: at.Add(time.Duration(l) * time.Second), Source: "/var/log/app.log",
				Level: domain.LevelInfo, Message: fmt.Sprintf("request %d served in %d ms for host %d", l, 10+l, host)})
		}
		p.LogStats = ingest.LogStats{Received: 5, Bytes: 250}
		return p, nil
	}
}

func envInt(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return def
}
