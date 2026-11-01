package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/MerhiOPS/sentrix/internal/app/alerting"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// capture serves a TLS endpoint and records every JSON body posted to it.
func capture(t *testing.T) (*Notifier, string, *[]json.RawMessage) {
	t.Helper()
	var got []json.RawMessage
	srv := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var raw json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&raw)
		got = append(got, raw)
	}))
	t.Cleanup(srv.Close)
	return testNotifier(srv), srv.URL, &got
}

func monitorMsg(ev domain.MonitorEventKind) alerting.Message {
	exp := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	return alerting.Message{At: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), Monitor: &alerting.MonitorMessage{
		Name: "shop <!channel>", Target: "https://shop.example.com/health", Event: ev, Class: domain.ErrTimeout,
		DownFor: 3*time.Minute + 400*time.Millisecond, CertExpiry: &exp}}
}

// TestWebhookBodiesMatchTheDocumentedSchemas posts every kind of message
// and validates each body against docs/openapi.yaml: a receiver written to
// the documentation must be able to read what is sent.
func TestWebhookBodiesMatchTheDocumentedSchemas(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromFile("../../../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	n, url, got := capture(t)
	ch := domain.Channel{Kind: domain.ChannelWebhook, URL: url}
	msgs := []alerting.Message{msg(), monitorMsg(domain.EventDown), monitorMsg(domain.EventUp),
		monitorMsg(domain.EventCertExpiring), monitorMsg(domain.EventCertRenewed)}
	for _, m := range msgs {
		if err := n.Notify(context.Background(), ch, m); err != nil {
			t.Fatal(err)
		}
	}
	for i, raw := range *got {
		schema := "MonitorWebhook"
		if i == 0 {
			schema = "AlertWebhook"
		}
		var v any
		_ = json.Unmarshal(raw, &v)
		if err := doc.Components.Schemas[schema].Value.VisitJSON(v); err != nil {
			t.Errorf("body %d does not match %s: %v\n%s", i, schema, err, raw)
		}
	}
	var up map[string]any
	_ = json.Unmarshal((*got)[2], &up)
	if up["down_for_s"] != float64(180) || up["resolved"] != true || up["source"] != "monitor" {
		t.Fatalf("up body %v", up)
	}
	var down map[string]any
	_ = json.Unmarshal((*got)[1], &down)
	if down["error"] != "timeout" || down["resolved"] != false || down["down_for_s"] != nil {
		t.Fatalf("down body %v", down)
	}
}

func TestSlackMonitorText(t *testing.T) {
	n, url, got := capture(t)
	ch := domain.Channel{Kind: domain.ChannelSlack, URL: url}
	want := map[domain.MonitorEventKind]string{
		domain.EventDown:         ":red_circle: DOWN — shop &lt;!channel&gt; (https://shop.example.com/health): timed out",
		domain.EventUp:           ":large_green_circle: UP — shop &lt;!channel&gt; (https://shop.example.com/health) after 3m0s down",
		domain.EventCertExpiring: "CERTIFICATE EXPIRING — shop &lt;!channel&gt; (https://shop.example.com/health) on 2026-11-14 (13 days)",
		domain.EventCertRenewed:  "CERTIFICATE RENEWED — shop &lt;!channel&gt; (https://shop.example.com/health), valid until 2026-11-14",
	}
	order := []domain.MonitorEventKind{domain.EventDown, domain.EventUp, domain.EventCertExpiring, domain.EventCertRenewed}
	for _, ev := range order {
		if err := n.Notify(context.Background(), ch, monitorMsg(ev)); err != nil {
			t.Fatal(err)
		}
	}
	for i, ev := range order {
		var body map[string]string
		_ = json.Unmarshal((*got)[i], &body)
		if !strings.Contains(body["text"], want[ev]) || strings.Contains(body["text"], "<!channel>") {
			t.Errorf("%s: %q, want it to contain %q", ev, body["text"], want[ev])
		}
	}
}
