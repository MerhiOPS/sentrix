package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/uptime"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// fakeSched records what the admin use case tells the scheduler.
type fakeSched struct {
	mu        sync.Mutex
	refreshes int
	checked   []int64
	busy      bool
}

func (f *fakeSched) Refresh() { f.mu.Lock(); f.refreshes++; f.mu.Unlock() }
func (f *fakeSched) CheckNow(id int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.busy {
		return false
	}
	f.checked = append(f.checked, id)
	return true
}

const shopBody = `{"name":"shop","kind":"http","target":"https://shop.example.com/health","interval_s":60}`

func (r *rig) createMonitor(t *testing.T, cookie, body string) monitorDTO {
	t.Helper()
	res := r.call(t, "POST", "/api/v1/monitors", cookie, body)
	if res.status != http.StatusCreated {
		t.Fatalf("create monitor: %d %s", res.status, res.body)
	}
	var m monitorDTO
	_ = json.Unmarshal(res.body, &m)
	return m
}

func TestCreateMonitorAppliesDefaults(t *testing.T) {
	r := newRig(t)
	m := r.createMonitor(t, r.login(t, r.a.admin), shopBody)
	if m.Method != "GET" || m.TimeoutS != 10 || m.Retries != 1 || m.CertWarnDays != 14 || m.AcceptStatus != "200-399" ||
		m.State.Status != "pending" || m.State.LastCheck != nil || len(m.Uptime) != 0 || len(m.Bars) != uptime.DetailBars {
		t.Fatalf("created %+v", m)
	}
	if r.sched.refreshes != 1 {
		t.Fatalf("the scheduler was refreshed %d times, want once so the first check runs at once", r.sched.refreshes)
	}
	tcp := r.createMonitor(t, r.login(t, r.a.admin), `{"name":"db","kind":"tcp","target":"db.example.com:5432","interval_s":120,"timeout_s":3,"confirm_retries":0}`)
	if tcp.Method != "" || tcp.CertWarnDays != 0 || tcp.TimeoutS != 3 || tcp.Retries != 0 || tcp.AcceptStatus != "" {
		t.Fatalf("tcp monitor %+v", tcp)
	}
}

func TestMonitorInputIsValidated(t *testing.T) {
	r := newRig(t)
	admin := r.login(t, r.a.admin)
	bad := map[string]string{
		"below the plan's interval": `{"name":"x","kind":"http","target":"https://a.example","interval_s":30}`,
		"not a URL":                 `{"name":"x","kind":"http","target":"a.example","interval_s":60}`,
		"credentials in the URL":    `{"name":"x","kind":"http","target":"https://u:p@a.example","interval_s":60}`,
		"http option on tcp":        `{"name":"x","kind":"tcp","target":"a.example:22","interval_s":60,"method":"GET"}`,
		"unknown field":             `{"name":"x","kind":"http","target":"https://a.example","interval_s":60,"headers":{"a":"b"}}`,
		"bad status ranges":         `{"name":"x","kind":"http","target":"https://a.example","interval_s":60,"accept_status":"2xx"}`,
		"keyword on HEAD":           `{"name":"x","kind":"http","target":"https://a.example","interval_s":60,"method":"HEAD","keyword":"ok","keyword_mode":"present"}`,
	}
	for name, body := range bad {
		if res := r.call(t, "POST", "/api/v1/monitors", admin, body); res.status != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, res.status, res.body)
		}
	}
	r.noPing = errors.New("ping_group_range excludes this process")
	res := r.call(t, "POST", "/api/v1/monitors", admin, `{"name":"x","kind":"ping","target":"203.0.113.9","interval_s":60}`)
	if res.status != http.StatusBadRequest || !strings.Contains(string(res.body), "ping monitors cannot run") {
		t.Fatalf("ping without ping sockets: %d %s, want 400 with the reason", res.status, res.body)
	}
	var list monitorListDTO
	_ = json.Unmarshal(r.call(t, "GET", "/api/v1/monitors", admin, "").body, &list)
	if list.PingAvailable || len(list.Monitors) != 0 {
		t.Fatalf("list %+v: nothing stored, ping reported unavailable", list)
	}
}

func TestMonitorPlanLimit(t *testing.T) {
	r := newRig(t)
	ops, _ := domain.NewOpsScope("test")
	plan := domain.DefaultPlan
	plan.MaxMonitors, plan.MinCheckInterval = 1, 30*time.Second
	if err := r.store.SetPlan(context.Background(), ops, r.a.tenant, plan); err != nil {
		t.Fatal(err)
	}
	admin := r.login(t, r.a.admin)
	r.createMonitor(t, admin, `{"name":"fast","kind":"http","target":"https://a.example","interval_s":30}`)
	if res := r.call(t, "POST", "/api/v1/monitors", admin, shopBody); res.status != http.StatusTooManyRequests {
		t.Fatalf("over max_monitors: %d, want 429", res.status)
	}
	var u usageDTO
	_ = json.Unmarshal(r.call(t, "GET", "/api/v1/usage", admin, "").body, &u)
	if u.Plan.MaxMonitors != 1 || u.Plan.MinCheckIntervalS != 30 || u.Usage.Monitors != 1 {
		t.Fatalf("usage %+v", u)
	}
}

func TestViewerReadsMonitorsButCannotChangeThem(t *testing.T) {
	r := newRig(t)
	m := r.createMonitor(t, r.login(t, r.a.admin), shopBody)
	viewer := r.login(t, r.a.viewer)
	if res := r.call(t, "GET", fmt.Sprintf("/api/v1/monitors/%d", m.ID), viewer, ""); res.status != http.StatusOK {
		t.Fatalf("viewer get: %d", res.status)
	}
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/api/v1/monitors", shopBody},
		{"PUT", fmt.Sprintf("/api/v1/monitors/%d", m.ID), shopBody},
		{"DELETE", fmt.Sprintf("/api/v1/monitors/%d", m.ID), ""},
		{"POST", fmt.Sprintf("/api/v1/monitors/%d/pause", m.ID), ""},
		{"POST", fmt.Sprintf("/api/v1/monitors/%d/check", m.ID), ""},
	} {
		if res := r.call(t, c.method, c.path, viewer, c.body); res.status != http.StatusForbidden {
			t.Errorf("viewer %s %s: %d, want 403", c.method, c.path, res.status)
		}
	}
}

func TestAnotherTenantsMonitorIsNotFound(t *testing.T) {
	r := newRig(t)
	m := r.createMonitor(t, r.login(t, r.a.admin), shopBody)
	other := r.login(t, r.b.admin)
	base := fmt.Sprintf("/api/v1/monitors/%d", m.ID)
	for _, c := range []struct{ method, path, body string }{
		{"GET", base, ""}, {"PUT", base, shopBody}, {"DELETE", base, ""}, {"POST", base + "/pause", ""},
		{"POST", base + "/resume", ""}, {"POST", base + "/check", ""}, {"GET", base + "/incidents", ""},
		{"GET", base + "/results?from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z", ""},
	} {
		if res := r.call(t, c.method, c.path, other, c.body); res.status != http.StatusNotFound {
			t.Errorf("tenant B %s %s: %d, want 404", c.method, c.path, res.status)
		}
	}
	got, err := r.store.GetMonitor(context.Background(), r.a.scope, m.ID)
	if err != nil || got.Name != "shop" || got.Paused || len(r.sched.checked) != 0 {
		t.Fatalf("tenant A's monitor after B's attempts: %+v, %v, checks %v", got, err, r.sched.checked)
	}
	res := r.call(t, "POST", "/api/v1/silences", other, fmt.Sprintf(`{"monitor_id":%d,"starts":"2026-01-01T00:00:00Z","ends":"2026-01-01T01:00:00Z"}`, m.ID))
	if res.status != http.StatusNotFound {
		t.Fatalf("tenant B silencing A's monitor: %d, want 404", res.status)
	}
}

func TestPauseResumeAndCheckNow(t *testing.T) {
	r := newRig(t)
	admin := r.login(t, r.a.admin)
	m := r.createMonitor(t, admin, shopBody)
	base := fmt.Sprintf("/api/v1/monitors/%d", m.ID)
	if res := r.call(t, "POST", base+"/check", admin, ""); res.status != http.StatusAccepted {
		t.Fatalf("check now: %d %s", res.status, res.body)
	}
	if res := r.call(t, "POST", base+"/check", admin, ""); res.status != http.StatusTooManyRequests {
		t.Fatalf("second check within 10 s: %d, want 429", res.status)
	}
	if res := r.call(t, "POST", base+"/pause", admin, ""); res.status != http.StatusNoContent {
		t.Fatalf("pause: %d", res.status)
	}
	var got monitorDTO
	_ = json.Unmarshal(r.call(t, "GET", base, admin, "").body, &got)
	if !got.Paused || got.State.Status != "paused" {
		t.Fatalf("after pause %+v", got)
	}
	if res := r.call(t, "POST", base+"/check", admin, ""); res.status != http.StatusConflict {
		t.Fatalf("check a paused monitor: %d, want 409", res.status)
	}
	if res := r.call(t, "POST", base+"/resume", admin, ""); res.status != http.StatusNoContent {
		t.Fatalf("resume: %d", res.status)
	}
	_ = json.Unmarshal(r.call(t, "GET", base, admin, "").body, &got)
	if got.Paused || got.State.Status != "pending" || len(r.sched.checked) != 1 {
		t.Fatalf("after resume %+v, checks %v", got, r.sched.checked)
	}
}

func TestMonitorHistoryMatchesTheSpec(t *testing.T) {
	r := newRig(t)
	admin := r.login(t, r.a.admin)
	created := r.createMonitor(t, admin, shopBody)
	ctx := context.Background()
	m, _ := r.store.GetMonitor(ctx, r.a.scope, created.ID)
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-10 * time.Minute)
	exp := now.Add(40 * 24 * time.Hour)
	for i, c := range []struct {
		o     domain.CheckOutcome
		class domain.ErrorClass
	}{{domain.CheckUp, ""}, {domain.CheckUp, ""}, {domain.CheckDown, domain.ErrTimeout}, {domain.CheckDown, domain.ErrTimeout},
		{domain.CheckDown, domain.ErrTimeout}, {domain.CheckUnknown, domain.ErrConnect}, {domain.CheckUp, ""}} {
		states, _ := r.store.MonitorStates(ctx, r.a.scope)
		res := domain.CheckResult{At: start.Add(time.Duration(i) * time.Minute), Outcome: c.o, Class: c.class, Latency: 80 * time.Millisecond,
			StatusCode: 200, TTFB: 30 * time.Millisecond, CertExpiry: &exp}
		if c.o != domain.CheckUp {
			res.StatusCode, res.TTFB, res.Latency, res.CertExpiry = 0, 0, 0, nil
		}
		step := domain.StepMonitor(m, states[0], res)
		if _, err := r.store.RecordCheck(ctx, r.a.scope, uptime.CheckRecord{Monitor: m, Result: res, Step: step, Revision: states[0].Revision}); err != nil {
			t.Fatal(err)
		}
	}
	var got monitorDTO
	_ = json.Unmarshal(r.call(t, "GET", fmt.Sprintf("/api/v1/monitors/%d", m.ID), admin, "").body, &got)
	if u := got.Uptime["24h"]; u <= 0 || u >= 1 || got.State.Status != "up" || got.AvgLatencyMS == nil || *got.AvgLatencyMS != 80 {
		t.Fatalf("monitor %+v: uptime strictly between 0 and 1 after an incident", got)
	}
	if got.State.CertStatus == nil || *got.State.CertStatus != "ok" || got.State.CertExpiresAt == nil {
		t.Fatalf("certificate state %+v", got.State)
	}
	from, to := start.Add(-time.Minute).Format(time.RFC3339), now.Format(time.RFC3339)
	res := r.call(t, "GET", fmt.Sprintf("/api/v1/monitors/%d/results?from=%s&to=%s", m.ID, from, to), admin, "")
	var results struct{ Results []checkResultDTO }
	_ = json.Unmarshal(res.body, &results)
	if res.status != http.StatusOK || len(results.Results) != 7 || results.Results[5].Outcome != "unknown" || results.Results[5].LatencyMS != nil {
		t.Fatalf("results %d %s", res.status, res.body)
	}
	res = r.call(t, "GET", fmt.Sprintf("/api/v1/monitors/%d/incidents?limit=10", m.ID), admin, "")
	var hist incidentListDTO
	_ = json.Unmarshal(res.body, &hist)
	if len(hist.Incidents) != 1 || hist.Incidents[0].Ended == nil || hist.Incidents[0].Error != "timeout" || len(hist.Events) != 2 {
		t.Fatalf("incidents %s", res.body)
	}
	if hist.Events[0].Kind != "up" || hist.Events[0].DownForS == nil || hist.Events[1].Kind != "down" || hist.Events[1].Notified {
		t.Fatalf("events %+v: up then down, not notified (no channels)", hist.Events)
	}
	for _, q := range []string{"?from=x&to=y", "?from=" + to + "&to=" + from, "?from=2026-01-01T00:00:00Z&to=2026-03-01T00:00:00Z"} {
		if res := r.call(t, "GET", fmt.Sprintf("/api/v1/monitors/%d/results%s", m.ID, q), admin, ""); res.status != http.StatusBadRequest {
			t.Errorf("results %s: %d, want 400", q, res.status)
		}
	}
}

func TestExportUptimeTables(t *testing.T) {
	r := newRig(t)
	admin := r.login(t, r.a.admin)
	created := r.createMonitor(t, admin, shopBody)
	ctx := context.Background()
	m, _ := r.store.GetMonitor(ctx, r.a.scope, created.ID)
	st, _ := r.store.MonitorStates(ctx, r.a.scope)
	res := domain.CheckResult{At: time.Now().UTC().Add(-time.Minute), Outcome: domain.CheckDown, Class: domain.ErrDNS}
	if _, err := r.store.RecordCheck(ctx, r.a.scope, uptime.CheckRecord{Monitor: m, Result: res,
		Step: domain.StepMonitor(m, st[0], res), Revision: st[0].Revision}); err != nil {
		t.Fatal(err)
	}
	q := "&from=" + time.Now().Add(-time.Hour).UTC().Format(time.RFC3339) + "&to=" + time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	out := r.call(t, "GET", "/api/v1/export?table=monitor_results&format=csv"+q, admin, "")
	if out.status != http.StatusOK || !strings.Contains(string(out.body), "monitor_id,monitor,at,outcome,error") ||
		!strings.Contains(string(out.body), ",shop,") || !strings.Contains(string(out.body), ",down,dns,") {
		t.Fatalf("export: %d %s", out.status, out.body)
	}
	if res := r.call(t, "GET", "/api/v1/export?table=monitor_results&host="+r.a.host.String()+q, admin, ""); res.status != http.StatusBadRequest {
		t.Fatalf("an uptime table with a host: %d, want 400", res.status)
	}
	if other := r.call(t, "GET", "/api/v1/export?table=monitor_results&format=csv"+q, r.login(t, r.b.admin), ""); strings.Contains(string(other.body), "shop") {
		t.Fatal("tenant B's export contains tenant A's monitor")
	}
}
