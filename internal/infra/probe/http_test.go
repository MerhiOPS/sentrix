package probe

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/netguard"
)

func httpMon(url string) domain.Monitor {
	return domain.Monitor{Kind: domain.MonitorHTTP, Target: url, Method: "GET", Timeout: 2 * time.Second}
}

// loopback admits loopback targets (the test servers).
func loopback() *Prober { return New(netguard.Policy{AllowPrivate: true}) }

func expect(t *testing.T, r domain.CheckResult, o domain.CheckOutcome, c domain.ErrorClass) {
	t.Helper()
	if r.Outcome != o || r.Class != c {
		t.Fatalf("result %+v, want %s/%q", r, o, c)
	}
}

func TestHTTPStatusAndTiming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != userAgent {
			w.WriteHeader(http.StatusTeapot)
			return
		}
		switch r.URL.Path {
		case "/down":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/moved":
			http.Redirect(w, r, "/", http.StatusFound)
		default:
			_, _ = w.Write([]byte("ok"))
		}
	}))
	defer srv.Close()
	p := loopback()
	r := p.Probe(context.Background(), httpMon(srv.URL))
	expect(t, r, domain.CheckUp, domain.ErrNone)
	if r.StatusCode != 200 || r.Latency <= 0 || r.TTFB <= 0 || r.TTFB > r.Latency || r.At.IsZero() || r.CertExpiry != nil {
		t.Fatalf("result %+v", r)
	}
	r = p.Probe(context.Background(), httpMon(srv.URL+"/down"))
	expect(t, r, domain.CheckDown, domain.ErrStatus)
	if r.StatusCode != 503 {
		t.Fatalf("status %d", r.StatusCode)
	}
	if r := p.Probe(context.Background(), httpMon(srv.URL+"/moved")); r.Outcome != domain.CheckUp || r.StatusCode != 200 {
		t.Fatalf("redirect not followed to the final status: %+v", r)
	}
	m := httpMon(srv.URL + "/down")
	m.AcceptStatus = []domain.StatusRange{{From: 503, To: 503}}
	expect(t, p.Probe(context.Background(), m), domain.CheckUp, domain.ErrNone)
}

func TestHTTPKeyword(t *testing.T) {
	big := strings.Repeat("x", MaxBody) + "Welcome"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/big" {
			_, _ = w.Write([]byte(big))
			return
		}
		_, _ = w.Write([]byte("<h1>Welcome back</h1>"))
	}))
	defer srv.Close()
	p := loopback()
	cases := []struct {
		path, kw string
		mode     domain.KeywordMode
		caseSens bool
		want     domain.CheckOutcome
	}{
		{"/", "Welcome", domain.KeywordPresent, true, domain.CheckUp},
		{"/", "welcome", domain.KeywordPresent, true, domain.CheckDown},
		{"/", "welcome", domain.KeywordPresent, false, domain.CheckUp},
		{"/", "Maintenance", domain.KeywordAbsent, false, domain.CheckUp},
		{"/", "WELCOME", domain.KeywordAbsent, false, domain.CheckDown},
		{"/big", "Welcome", domain.KeywordPresent, true, domain.CheckDown}, // past the 1 MiB cap
	}
	for _, c := range cases {
		m := httpMon(srv.URL + c.path)
		m.Keyword, m.KeywordMode, m.KeywordCase = c.kw, c.mode, c.caseSens
		r := p.Probe(context.Background(), m)
		if r.Outcome != c.want || (c.want == domain.CheckDown && r.Class != domain.ErrKeyword) {
			t.Errorf("%s %s %q case=%v: %+v, want %s", c.path, c.mode, c.kw, c.caseSens, r, c.want)
		}
	}
}

func TestHTTPRefusesBlockedAddressesBeforeConnecting(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	strict := New(netguard.Strict)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	for _, u := range []string{srv.URL, "http://localhost:" + port + "/"} {
		expect(t, strict.Probe(context.Background(), httpMon(u)), domain.CheckDown, domain.ErrBlocked)
	}
	if hits.Load() != 0 {
		t.Fatalf("a blocked target was reached %d times", hits.Load())
	}
}

// A public page that redirects to an internal address is refused at the
// hop: the dialer checks every connection, not just the first URL.
func TestHTTPRedirectToBlockedAddressRefused(t *testing.T) {
	var internalHits atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { internalHits.Add(1) }))
	defer internal.Close()
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL+"/admin", http.StatusFound)
	}))
	defer public.Close()
	p := New(netguard.Strict)
	publicAddr := public.Listener.Addr().String()
	// Stand-in for a public address: only the "public" server's port passes.
	p.control = func(network, address string, c syscall.RawConn) error {
		if address == publicAddr {
			return nil
		}
		return netguard.Strict.Control(network, address, c)
	}
	expect(t, p.Probe(context.Background(), httpMon(public.URL)), domain.CheckDown, domain.ErrBlocked)
	if internalHits.Load() != 0 {
		t.Fatal("the redirect target behind the guard was reached")
	}
}

func TestHTTPFailureClasses(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	loop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path+"x", http.StatusFound)
	}))
	defer loop.Close()
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	p := loopback()
	m := httpMon(slow.URL)
	m.Timeout = 300 * time.Millisecond
	expect(t, p.Probe(context.Background(), m), domain.CheckDown, domain.ErrTimeout)
	expect(t, p.Probe(context.Background(), httpMon(loop.URL)), domain.CheckDown, domain.ErrStatus)
	expect(t, p.Probe(context.Background(), httpMon(closedURL)), domain.CheckDown, domain.ErrConnect)
	expect(t, p.Probe(context.Background(), httpMon("http://sentrix-uptime-test.invalid/")), domain.CheckDown, domain.ErrDNS)
}

func TestHTTPIgnoresEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	if loopback().client().Transport.(*http.Transport).Proxy != nil {
		t.Fatal("the transport honours a proxy, which would dial past the guard")
	}
}
