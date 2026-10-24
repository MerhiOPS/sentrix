package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestProbes(t *testing.T) {
	r := newRig(t)
	if res := r.call(t, "GET", "/healthz", "", ""); res.status != http.StatusOK {
		t.Fatalf("healthz: %d", res.status)
	}
	if res := r.call(t, "GET", "/readyz", "", ""); res.status != http.StatusOK {
		t.Fatalf("readyz: %d", res.status)
	}
	r.notReady = errors.New("writer: context deadline exceeded")
	if res := r.call(t, "GET", "/readyz", "", ""); res.status != http.StatusServiceUnavailable {
		t.Fatalf("readyz with a stuck writer: %d, want 503", res.status)
	}
	if res := r.call(t, "GET", "/healthz", "", ""); res.status != http.StatusOK {
		t.Fatal("liveness must not depend on the database")
	}
}

func TestSystemReportIsAdminOnlyAndShaped(t *testing.T) {
	r := newRig(t)
	if res := r.call(t, "GET", "/api/v1/system", r.login(t, r.a.viewer), ""); res.status != http.StatusForbidden {
		t.Fatalf("viewer: %d, want 403", res.status)
	}
	r.ingestSamples(t, r.a, 1, time.Now())
	res := r.call(t, "GET", "/api/v1/system", r.login(t, r.a.admin), "")
	var sys systemDTO
	_ = json.Unmarshal(res.body, &sys)
	if res.status != http.StatusOK || sys.Fleet.Hosts != 1 || sys.Fleet.Reporting != 1 || sys.Server.DBBytes != 4096 {
		t.Fatalf("system: %d %s", res.status, res.body)
	}
}
