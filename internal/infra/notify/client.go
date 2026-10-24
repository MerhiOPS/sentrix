package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"
)

// Limits on one delivery.
const (
	RequestTimeout  = 10 * time.Second
	maxResponseBody = 64 << 10
	maxRequestBody  = 64 << 10
)

// poster sends one JSON body to a URL through the guarded client.
type poster struct {
	client *http.Client
}

// newPoster builds the guarded client. ctl is the dial Control hook (the
// SSRF guard; tests substitute one admitting their loopback server) and
// tlsConf the client TLS config (nil: system roots).
func newPoster(ctl func(string, string, syscall.RawConn) error, tlsConf *tls.Config) *poster {
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: ctl}
	tr := &http.Transport{
		Proxy:                  nil, // an environment proxy would dial for us, past the guard
		DialContext:            dialer.DialContext,
		TLSClientConfig:        tlsConf,
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  RequestTimeout,
		MaxResponseHeaderBytes: 16 << 10,
		MaxIdleConns:           10,
		IdleConnTimeout:        30 * time.Second,
		ForceAttemptHTTP2:      true,
	}
	return &poster{client: &http.Client{
		Transport: tr,
		Timeout:   RequestTimeout,
		// A redirect is a second destination nobody validated as a URL;
		// the guard would still check its address, but a webhook that
		// redirects is misconfigured either way.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// post sends body and requires a 2xx. Errors never include the URL: a
// Slack webhook URL is a credential, and errors end up on the event.
func (p *poster) post(ctx context.Context, rawURL string, body []byte) error {
	if len(body) > maxRequestBody {
		return errors.New("notification too large")
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return errors.New("channel URL must be https")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return errors.New("bad channel URL")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "sentrix-alerts")
	resp, err := p.client.Do(req)
	if err != nil {
		return stripURL(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBody))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("destination answered %d", resp.StatusCode)
	}
	return nil
}

func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
