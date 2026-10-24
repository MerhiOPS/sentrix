package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLoginMeAndLogout(t *testing.T) {
	r := newRig(t)
	c := r.login(t, r.a.admin)
	res := r.call(t, "GET", "/api/v1/me", c, "")
	var me meDTO
	_ = json.Unmarshal(res.body, &me)
	if res.status != 200 || me.Email != r.a.admin || me.Role != "admin" || me.TenantID != r.a.tenant.String() {
		t.Fatalf("me: %d %+v", res.status, me)
	}
	if res := r.call(t, "POST", "/api/v1/auth/logout", c, ""); res.status != http.StatusNoContent {
		t.Fatalf("logout: %d", res.status)
	}
	if res := r.call(t, "GET", "/api/v1/me", c, ""); res.status != http.StatusUnauthorized {
		t.Fatalf("session usable after logout: %d", res.status)
	}
}

func TestLoginFailureIsGeneric401(t *testing.T) {
	r := newRig(t)
	wrong := r.call(t, "POST", "/api/v1/auth/login", "", `{"email":"`+r.a.admin+`","password":"nope nope nope"}`)
	unknown := r.call(t, "POST", "/api/v1/auth/login", "", `{"email":"ghost@example.com","password":"nope nope nope"}`)
	if wrong.status != 401 || unknown.status != 401 || string(wrong.body) != string(unknown.body) {
		t.Fatalf("login failures distinguishable: %d %s / %d %s", wrong.status, wrong.body, unknown.status, unknown.body)
	}
}

func TestViewerCannotCallAdminEndpoints(t *testing.T) {
	r := newRig(t)
	if res := r.call(t, "POST", "/api/v1/enrollment-tokens", r.login(t, r.a.viewer), ""); res.status != http.StatusForbidden {
		t.Fatalf("viewer created a token: %d %s", res.status, res.body)
	}
	if res := r.call(t, "POST", "/api/v1/enrollment-tokens", r.login(t, r.a.admin), ""); res.status != http.StatusCreated {
		t.Fatalf("admin token: %d %s", res.status, res.body)
	}
}

func TestOtherTenantsHostIs404Everywhere(t *testing.T) {
	r := newRig(t)
	now := time.Now()
	r.ingestSamples(t, r.a, 1, now)
	cb := r.login(t, r.b.viewer)
	q := "?gauges=cpu&from=" + url.QueryEscape(now.Add(-time.Minute).Format(time.RFC3339)) + "&to=" + url.QueryEscape(now.Add(time.Minute).Format(time.RFC3339))
	for _, p := range []string{"/api/v1/hosts/" + r.a.host.String(), "/api/v1/hosts/" + r.a.host.String() + "/metrics" + q} {
		res := r.call(t, "GET", p, cb, "")
		ghost := r.call(t, "GET", strings.Replace(p, r.a.host.String(), "00000000-0000-4000-8000-000000000000", 1), cb, "")
		if res.status != http.StatusNotFound || string(res.body) != string(ghost.body) {
			t.Fatalf("%s as tenant B: %d %s (unknown id gives %d %s)", p, res.status, res.body, ghost.status, ghost.body)
		}
	}
	res := r.call(t, "GET", "/api/v1/hosts", cb, "")
	if strings.Contains(string(res.body), r.a.host.String()) {
		t.Fatal("tenant B's host list contains tenant A's host")
	}
}

func TestHostListShowsLatestAndStatus(t *testing.T) {
	r := newRig(t)
	r.ingestSamples(t, r.a, 1, time.Now().Add(-5*time.Second), time.Now())
	res := r.call(t, "GET", "/api/v1/hosts", r.login(t, r.a.viewer), "")
	var list hostListDTO
	_ = json.Unmarshal(res.body, &list)
	if res.status != 200 || len(list.Hosts) != 1 || list.Hosts[0].Status != "ok" || list.Hosts[0].Latest == nil || list.Hosts[0].Latest.CPU != 42 {
		t.Fatalf("host list: %d %s", res.status, res.body)
	}
}

func TestMetricsPointCapAndNullGaps(t *testing.T) {
	r := newRig(t)
	now := time.Now().Truncate(5 * time.Second)
	r.ingestSamples(t, r.a, 1, now.Add(-20*time.Second), now.Add(-5*time.Second))
	c := r.login(t, r.a.viewer)
	base := "/api/v1/hosts/" + r.a.host.String() + "/metrics?gauges=cpu,mem"
	span := func(from, to time.Time) string {
		return "&from=" + url.QueryEscape(from.Format(time.RFC3339)) + "&to=" + url.QueryEscape(to.Format(time.RFC3339))
	}
	res := r.call(t, "GET", base+span(now.Add(-20*time.Second), now), c, "")
	var f frameDTO
	_ = json.Unmarshal(res.body, &f)
	if res.status != 200 || len(f.TSMS) != 4 || f.Series["cpu"][0] == nil || f.Series["cpu"][1] != nil {
		t.Fatalf("frame: %d %s (gaps must be null)", res.status, res.body)
	}
	day := span(now.Add(-24*time.Hour), now)
	if res := r.call(t, "GET", base+day, c, ""); res.status != http.StatusBadRequest {
		t.Fatalf("a day at natural resolution without step: %d, want 400", res.status)
	}
	if res := r.call(t, "GET", base+day+"&step=1", c, ""); res.status != http.StatusBadRequest {
		t.Fatalf("a day at step=1: %d, want 400", res.status)
	}
	if res := r.call(t, "GET", base+day+"&step=60", c, ""); res.status != http.StatusOK {
		t.Fatalf("a day at step=60: %d %s", res.status, res.body)
	}
}

func TestStateChangingRequestsRequireSameOrigin(t *testing.T) {
	r := newRig(t)
	body := strings.NewReader(`{"email":"` + r.a.admin + `","password":"` + testPassword + `"}`)
	for name, origin := range map[string]string{"missing": "", "foreign": "https://evil.example"} {
		req, _ := http.NewRequest("POST", r.srv.URL+"/api/v1/auth/login", body)
		req.Host = "sentrix.test"
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Errorf("%s Origin: %d, want 403", name, res.StatusCode)
		}
	}
}

func TestUnknownAPIPathIsJSON404AndSecurityHeaders(t *testing.T) {
	r := newRig(t)
	res := r.call(t, "GET", "/api/v1/nope", "", "")
	if res.status != 404 || !strings.Contains(res.header.Get("Content-Type"), "json") {
		t.Fatalf("unknown api path: %d %s", res.status, res.header.Get("Content-Type"))
	}
	for _, h := range []string{"Content-Security-Policy", "X-Frame-Options", "X-Content-Type-Options"} {
		if res.header.Get(h) == "" {
			t.Errorf("missing %s", h)
		}
	}
}
