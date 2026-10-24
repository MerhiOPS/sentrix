package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

func (r *rig) ingestDepth(t *testing.T, tr tenantRig, seq uint64, at time.Time) {
	t.Helper()
	b := ingest.Batch{Seq: seq,
		Details: []domain.DetailSample{
			{TS: at, Metric: domain.MetricCPUCore, Kind: domain.KindGauge, Labels: domain.Labels{"core": "0"}, Value: 33},
			{TS: at, Metric: domain.MetricNetRx, Kind: domain.KindCounter, Labels: domain.Labels{"nic": "eth0"}, Value: 1000},
		},
		Processes: []domain.ProcessSnapshot{{TS: at, Procs: []domain.Process{{Name: "postgres", CPUPercent: 12.5, RSS: 1 << 20}}}},
	}
	ah := ingest.AuthHost{Tenant: tr.tenant, Host: tr.host, Interval: 5 * time.Second, Enabled: true}
	if _, err := r.ingest.Ingest(context.Background(), ah, b); err != nil {
		t.Fatal(err)
	}
}

func rangeQuery(from, to time.Time) string {
	return "?from=" + url.QueryEscape(from.Format(time.RFC3339)) + "&to=" + url.QueryEscape(to.Format(time.RFC3339))
}

func TestSeriesListMetricsAndProcesses(t *testing.T) {
	r := newRig(t)
	now := time.Now().Truncate(5 * time.Second)
	r.ingestDepth(t, r.a, 1, now)
	c := r.login(t, r.a.viewer)
	base := "/api/v1/hosts/" + r.a.host.String()

	res := r.call(t, "GET", base+"/series", c, "")
	var list seriesListDTO
	_ = json.Unmarshal(res.body, &list)
	if res.status != 200 || len(list.Series) != 2 || list.Series[0].Metric != domain.MetricCPUCore || list.Series[0].Unit != "percent" {
		t.Fatalf("series: %d %s", res.status, res.body)
	}
	sid := strconv.FormatInt(list.Series[0].ID, 10)
	res = r.call(t, "GET", base+"/series/"+sid+"/metrics"+rangeQuery(now.Add(-time.Minute), now.Add(time.Minute)), c, "")
	var df detailFrameDTO
	_ = json.Unmarshal(res.body, &df)
	if res.status != 200 || df.Kind != "gauge" {
		t.Fatalf("series metrics: %d %s", res.status, res.body)
	}
	res = r.call(t, "GET", base+"/processes", c, "")
	var snap processSnapshotDTO
	_ = json.Unmarshal(res.body, &snap)
	if res.status != 200 || len(snap.Procs) != 1 || snap.Procs[0].Name != "postgres" {
		t.Fatalf("processes: %d %s", res.status, res.body)
	}
}

func TestThirtyDayChartReadsHourlyRollup(t *testing.T) {
	r := newRig(t)
	now := time.Now()
	r.ingestSamples(t, r.a, 1, now.Add(-time.Minute), now.Add(-30*time.Second))
	q := "/api/v1/hosts/" + r.a.host.String() + "/metrics" + rangeQuery(now.Add(-30*24*time.Hour), now) + "&gauges=cpu,net_rx_rate"
	res := r.call(t, "GET", q, r.login(t, r.a.viewer), "")
	var f frameDTO
	_ = json.Unmarshal(res.body, &f)
	if res.status != 200 || f.Source != "1h" || len(f.TSMS) > 301 {
		t.Fatalf("30-day chart: %d source %q points %d", res.status, f.Source, len(f.TSMS))
	}
}

func TestDepthEndpointsAre404ForAnotherTenant(t *testing.T) {
	r := newRig(t)
	now := time.Now().Truncate(5 * time.Second)
	r.ingestDepth(t, r.a, 1, now)
	r.ingestDepth(t, r.b, 1, now)
	ca, cb := r.login(t, r.a.viewer), r.login(t, r.b.viewer)
	var aList seriesListDTO
	_ = json.Unmarshal(r.call(t, "GET", "/api/v1/hosts/"+r.a.host.String()+"/series", ca, "").body, &aList)
	aSID := strconv.FormatInt(aList.Series[0].ID, 10)
	span := rangeQuery(now.Add(-time.Minute), now.Add(time.Minute))
	for _, p := range []string{
		"/api/v1/hosts/" + r.a.host.String() + "/series",
		"/api/v1/hosts/" + r.a.host.String() + "/processes",
		"/api/v1/hosts/" + r.a.host.String() + "/series/" + aSID + "/metrics" + span,
		"/api/v1/hosts/" + r.b.host.String() + "/series/" + aSID + "/metrics" + span, // own host, A's series
	} {
		if res := r.call(t, "GET", p, cb, ""); res.status != http.StatusNotFound {
			t.Errorf("tenant B %s: %d %s, want 404", p, res.status, res.body)
		}
	}
}
