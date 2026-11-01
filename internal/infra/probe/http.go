package probe

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Limits on one HTTP check.
const (
	MaxRedirects = 5
	// MaxBody is how much of the body a keyword is searched in. A keyword
	// beyond it is not found, and the monitor's UI says so.
	MaxBody        = 1 << 20
	maxHeaderBytes = 64 << 10
	userAgent      = "Sentrix-Uptime/1.0 (+https://github.com/MerhiOPS/sentrix)"
)

var errTooManyRedirects = errors.New("too many redirects")

// client builds a client for one check. Nothing is reused between checks:
// a pooled connection would hide DNS, connect and TLS failures.
func (p *Prober) client() *http.Client {
	dialer := &net.Dialer{Control: p.control}
	tr := &http.Transport{
		Proxy:                  nil, // an environment proxy would dial for us, past the guard
		DialContext:            dialer.DialContext,
		DisableKeepAlives:      true,
		ForceAttemptHTTP2:      true,
		MaxResponseHeaderBytes: maxHeaderBytes,
		TLSClientConfig:        &tls.Config{RootCAs: p.roots, MinVersion: tls.VersionTLS12},
	}
	return &http.Client{
		Transport: tr,
		// Every hop is dialled through the same guarded dialer, so a
		// redirect to an internal address is refused like a direct URL.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > MaxRedirects {
				return errTooManyRedirects
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errTooManyRedirects
			}
			return nil
		},
	}
}

func (p *Prober) http(ctx context.Context, m domain.Monitor) domain.CheckResult {
	var first time.Time
	start := time.Now()
	trace := &httptrace.ClientTrace{GotFirstResponseByte: func() { first = time.Now() }}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), m.Method, m.Target, nil)
	if err != nil {
		return down(domain.ErrConnect)
	}
	req.Header.Set("User-Agent", userAgent)
	client := p.client()
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, errTooManyRedirects) {
			return down(domain.ErrStatus)
		}
		return down(classify(err))
	}
	defer func() { _ = resp.Body.Close() }()
	var body []byte
	if m.KeywordMode != domain.KeywordNone {
		body, err = io.ReadAll(io.LimitReader(resp.Body, MaxBody))
		if err != nil {
			return down(classify(err))
		}
	}
	r := domain.CheckResult{Outcome: domain.CheckUp, Latency: time.Since(start), StatusCode: resp.StatusCode,
		CertExpiry: chainExpiry(resp.TLS)}
	if !first.IsZero() {
		r.TTFB = first.Sub(start)
	}
	switch {
	case !m.Accepts(resp.StatusCode):
		r.Outcome, r.Class = domain.CheckDown, domain.ErrStatus
	case m.KeywordMode != domain.KeywordNone && keywordFound(body, m.Keyword, m.KeywordCase) != (m.KeywordMode == domain.KeywordPresent):
		r.Outcome, r.Class = domain.CheckDown, domain.ErrKeyword
	}
	return r
}

func keywordFound(body []byte, keyword string, caseSensitive bool) bool {
	if caseSensitive {
		return bytes.Contains(body, []byte(keyword))
	}
	return bytes.Contains(bytes.ToLower(body), bytes.ToLower([]byte(keyword)))
}

// chainExpiry is the earliest NotAfter across the verified chain: an
// expiring intermediate breaks the site just as an expiring leaf does.
func chainExpiry(cs *tls.ConnectionState) *time.Time {
	if cs == nil || len(cs.VerifiedChains) == 0 {
		return nil
	}
	var earliest time.Time
	for _, c := range cs.VerifiedChains[0] {
		if earliest.IsZero() || c.NotAfter.Before(earliest) {
			earliest = c.NotAfter
		}
	}
	return &earliest
}
