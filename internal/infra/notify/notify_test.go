package notify

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/alerting"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/netguard"
)

func msg() alerting.Message {
	v := 97.5
	return alerting.Message{Rule: "cpu <!channel>", Host: "web-1", State: domain.StateFiring, Metric: "cpu", Value: &v,
		At: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}
}

func TestRefusesLoopbackByAddressAndByName(t *testing.T) {
	hits := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()
	n := New()
	port := srv.URL[strings.LastIndex(srv.URL, ":"):]
	for _, u := range []string{srv.URL, "https://localhost" + port + "/hook"} {
		err := n.Notify(context.Background(), domain.Channel{Kind: domain.ChannelWebhook, URL: u}, msg())
		if !errors.Is(err, netguard.ErrBlocked) {
			t.Errorf("%s: err %v, want ErrBlocked", u, err)
		}
	}
	if hits != 0 {
		t.Fatalf("the loopback server was reached %d times", hits)
	}
}

// testNotifier admits the loopback test server and trusts its certificate.
func testNotifier(srv *httptest.Server) *Notifier {
	allow := func(string, string, syscall.RawConn) error { return nil }
	tlsConf := &tls.Config{RootCAs: srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs, MinVersion: tls.VersionTLS12}
	return &Notifier{p: newPoster(allow, tlsConf)}
}

func TestWebhookAndSlackBodies(t *testing.T) {
	var got []map[string]any
	srv := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		got = append(got, m)
	}))
	defer srv.Close()
	n := testNotifier(srv)
	for _, k := range []domain.ChannelKind{domain.ChannelWebhook, domain.ChannelSlack} {
		if err := n.Notify(context.Background(), domain.Channel{Kind: k, URL: srv.URL}, msg()); err != nil {
			t.Fatal(err)
		}
	}
	if got[0]["host"] != "web-1" || got[0]["value"] != 97.5 || got[0]["at"] != "2026-10-24T12:00:00Z" {
		t.Errorf("webhook body %v", got[0])
	}
	text, _ := got[1]["text"].(string)
	if !strings.Contains(text, "FIRING") || strings.Contains(text, "<!channel>") || !strings.Contains(text, "97.50") {
		t.Errorf("slack text %q", text)
	}
}

func TestNoRedirectNoURLInErrors(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			t.Error("a redirect was followed")
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer srv.Close()
	n := testNotifier(srv)
	secret := srv.URL + "/services/T/B/very-secret"
	err := n.Notify(context.Background(), domain.Channel{Kind: domain.ChannelSlack, URL: secret}, msg())
	if err == nil || !strings.Contains(err.Error(), "302") {
		t.Fatalf("err %v, want the redirect reported as a failure", err)
	}
	srv.Close()
	err = n.Notify(context.Background(), domain.Channel{Kind: domain.ChannelSlack, URL: secret}, msg())
	if err == nil || strings.Contains(err.Error(), "very-secret") {
		t.Fatalf("err %v: a transport error must not carry the URL", err)
	}
}

func TestEnvironmentProxyIgnored(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	if New().p.client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("the transport honours a proxy, which would dial past the guard")
	}
}
