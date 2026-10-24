package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

const slackSecret = "https://hooks.slack.com/services/T0001/B0001/tenant-a-very-secret"

func ruleBody(hosts ...domain.HostID) string {
	ids := make([]string, 0, len(hosts))
	for _, h := range hosts {
		ids = append(ids, `"`+h.String()+`"`)
	}
	return `{"name":"cpu high","kind":"threshold","metric":"cpu","aggregation":"avg","comparator":"gt","threshold":90,` +
		`"duration_s":300,"hosts":[` + strings.Join(ids, ",") + `],"on_no_data":"hold","cooldown_s":600,"channels":[],"enabled":true}`
}

// TestChannelSecretsMaskedForEveryRole: Phase 5 acceptance.
func TestChannelSecretsMaskedForEveryRole(t *testing.T) {
	r := newRig(t)
	admin := r.login(t, r.a.admin)
	res := r.call(t, "POST", "/api/v1/channels", admin, `{"name":"ops","kind":"slack","url":"`+slackSecret+`"}`)
	if res.status != http.StatusCreated {
		t.Fatalf("create channel: %d %s", res.status, res.body)
	}
	bodies := []string{string(res.body)}
	var created channelDTO
	_ = json.Unmarshal(res.body, &created)
	for _, who := range []string{r.a.admin, r.a.viewer} {
		res := r.call(t, "GET", "/api/v1/channels", r.login(t, who), "")
		if res.status != http.StatusOK {
			t.Fatalf("%s list channels: %d", who, res.status)
		}
		bodies = append(bodies, string(res.body))
	}
	// A rename sends the masked URL back; the response is masked again.
	res = r.call(t, "PUT", fmt.Sprintf("/api/v1/channels/%d", created.ID), admin,
		`{"name":"on-call","kind":"slack","url":"`+created.URL+`"}`)
	if res.status != http.StatusOK {
		t.Fatalf("rename: %d %s", res.status, res.body)
	}
	bodies = append(bodies, string(res.body))
	for _, b := range bodies {
		if strings.Contains(b, "tenant-a-very-secret") || strings.Contains(b, "/services/") {
			t.Fatalf("a response carries the channel secret: %s", b)
		}
	}
	cs, _ := r.store.ListChannels(context.Background(), r.a.scope)
	if len(cs) != 1 || cs[0].URL != slackSecret || cs[0].Name != "on-call" {
		t.Fatalf("stored channel %+v: the masked rename lost the secret", cs)
	}
}

// TestRuleOnAnotherTenantsHostRefused: Phase 5 acceptance.
func TestRuleOnAnotherTenantsHostRefused(t *testing.T) {
	r := newRig(t)
	admin := r.login(t, r.a.admin)
	if res := r.call(t, "POST", "/api/v1/alert-rules", admin, ruleBody(r.b.host)); res.status != http.StatusNotFound {
		t.Fatalf("rule on B's host: %d %s, want 404", res.status, res.body)
	}
	res := r.call(t, "POST", "/api/v1/alert-rules", admin, ruleBody(r.a.host))
	if res.status != http.StatusCreated {
		t.Fatalf("own rule: %d %s", res.status, res.body)
	}
	var rule alertRuleDTO
	_ = json.Unmarshal(res.body, &rule)
	path := fmt.Sprintf("/api/v1/alert-rules/%d", rule.ID)
	if res := r.call(t, "PUT", path, admin, ruleBody(r.b.host)); res.status != http.StatusNotFound {
		t.Fatalf("retarget onto B's host: %d, want 404", res.status)
	}
	bAdmin := r.login(t, r.b.admin)
	if res := r.call(t, "PUT", path, bAdmin, ruleBody(r.b.host)); res.status != http.StatusNotFound {
		t.Fatalf("B updating A's rule: %d, want 404", res.status)
	}
	if res := r.call(t, "DELETE", path, bAdmin, ""); res.status != http.StatusNotFound {
		t.Fatalf("B deleting A's rule: %d, want 404", res.status)
	}
	var list alertRuleListDTO
	_ = json.Unmarshal(r.call(t, "GET", "/api/v1/alert-rules", bAdmin, "").body, &list)
	if len(list.Rules) != 0 {
		t.Fatalf("B sees A's rules: %+v", list.Rules)
	}
}

func TestViewerCannotChangeAlerting(t *testing.T) {
	r := newRig(t)
	viewer := r.login(t, r.a.viewer)
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/api/v1/alert-rules", ruleBody()},
		{"POST", "/api/v1/channels", `{"name":"x","kind":"webhook","url":"https://x.example/"}`},
		{"POST", "/api/v1/silences", `{"starts":"2026-10-24T12:00:00Z","ends":"2026-10-24T13:00:00Z"}`},
		{"POST", "/api/v1/hosts/" + r.a.host.String() + "/disable", ""},
	} {
		if res := r.call(t, c.method, c.path, viewer, c.body); res.status != http.StatusForbidden {
			t.Errorf("viewer %s %s: %d, want 403", c.method, c.path, res.status)
		}
	}
	if res := r.call(t, "GET", "/api/v1/alert-rules", viewer, ""); res.status != http.StatusOK {
		t.Errorf("viewer cannot read rules: %d", res.status)
	}
}

func TestDisableHostRefusesAgentAndMakesRuleInert(t *testing.T) {
	r := newRig(t)
	admin := r.login(t, r.a.admin)
	if res := r.call(t, "POST", "/api/v1/alert-rules", admin, ruleBody(r.a.host)); res.status != http.StatusCreated {
		t.Fatalf("create: %d %s", res.status, res.body)
	}
	if res := r.call(t, "POST", "/api/v1/hosts/"+r.b.host.String()+"/disable", admin, ""); res.status != http.StatusNotFound {
		t.Fatalf("disable B's host: %d, want 404", res.status)
	}
	if res := r.call(t, "POST", "/api/v1/hosts/"+r.a.host.String()+"/disable", admin, ""); res.status != http.StatusNoContent {
		t.Fatalf("disable: %d %s", res.status, res.body)
	}
	var list alertRuleListDTO
	_ = json.Unmarshal(r.call(t, "GET", "/api/v1/alert-rules", admin, "").body, &list)
	if len(list.Rules) != 1 || !list.Rules[0].Inert {
		t.Fatalf("rule on a disabled host not shown inert: %+v", list.Rules)
	}
	h, _ := r.store.GetHost(context.Background(), r.a.scope, r.a.host)
	if h.Status(time.Now()) != domain.StatusDisabled {
		t.Fatalf("host status %s after disable", h.Status(time.Now()))
	}
	if res := r.call(t, "POST", "/api/v1/hosts/"+r.a.host.String()+"/enable", admin, ""); res.status != http.StatusNoContent {
		t.Fatalf("enable: %d", res.status)
	}
}

func TestSilencesAndAlertsShape(t *testing.T) {
	r := newRig(t)
	admin := r.login(t, r.a.admin)
	host := r.a.host.String()
	res := r.call(t, "POST", "/api/v1/silences", admin,
		`{"host_id":"`+host+`","starts":"2026-10-24T12:00:00Z","ends":"2026-10-24T14:00:00Z","reason":"patching"}`)
	if res.status != http.StatusCreated {
		t.Fatalf("create silence: %d %s", res.status, res.body)
	}
	var sil silenceDTO
	_ = json.Unmarshal(res.body, &sil)
	if res := r.call(t, "POST", "/api/v1/silences", r.login(t, r.b.admin),
		`{"host_id":"`+host+`","starts":"2026-10-24T12:00:00Z","ends":"2026-10-24T14:00:00Z"}`); res.status != http.StatusNotFound {
		t.Fatalf("B silencing A's host: %d, want 404", res.status)
	}
	if res := r.call(t, "POST", "/api/v1/silences", admin, `{"starts":"2026-10-24T12:00:00Z","ends":"2026-12-24T12:00:00Z"}`); res.status != http.StatusBadRequest {
		t.Fatalf("60-day silence: %d, want 400", res.status)
	}
	if res := r.call(t, "POST", "/api/v1/alert-rules", admin, strings.Replace(ruleBody(), `"enabled"`, `"enabeld"`, 1)); res.status != http.StatusBadRequest {
		t.Fatalf("misspelt field: %d, want 400", res.status)
	}
	rule, _ := r.store.CreateRule(context.Background(), r.a.scope, domain.AlertRule{Name: "x", Kind: domain.RuleNoData, Duration: time.Minute, Enabled: true})
	v := 12.5
	_, _ = r.store.RecordTransition(context.Background(), r.a.scope,
		domain.RuleHostState{Rule: rule, Host: r.a.host, State: domain.StateFiring, Since: time.Now()},
		&domain.AlertEvent{From: domain.StatePending, To: domain.StateFiring, At: time.Now(), Value: &v, Notify: true})
	var alerts alertsDTO
	res = r.call(t, "GET", "/api/v1/alerts?limit=10", r.login(t, r.a.viewer), "")
	_ = json.Unmarshal(res.body, &alerts)
	if res.status != http.StatusOK || len(alerts.States) != 1 || len(alerts.Events) != 1 || alerts.Events[0].Value == nil {
		t.Fatalf("alerts: %d %s", res.status, res.body)
	}
	if res := r.call(t, "DELETE", fmt.Sprintf("/api/v1/silences/%d", sil.ID), admin, ""); res.status != http.StatusNoContent {
		t.Fatalf("delete silence: %d", res.status)
	}
}
