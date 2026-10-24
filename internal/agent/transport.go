package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/MerhiOPS/sentrix/internal/infra/tlscert"
	"github.com/MerhiOPS/sentrix/internal/wire"
)

// requestTimeout bounds one send; a hung server must not stall the buffer.
const requestTimeout = 30 * time.Second

// maxResponseBytes bounds what the agent reads back.
const maxResponseBytes = 64 << 10

// NewClient builds the HTTP client for a server. With a pin, the server key
// must match it exactly; without one, https uses system roots. Plain http
// is accepted only because the server refuses to serve it anywhere but
// loopback (CheckListen) — the agent cannot know where it is listening.
func NewClient(serverURL string, pin []byte) (*http.Client, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("server url: %w", err)
	}
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     90 * time.Second,
		MaxIdleConns:        2,
	}
	switch {
	case u.Scheme == "https" && pin != nil:
		cfg, err := tlscert.PinnedClientConfig(pin)
		if err != nil {
			return nil, err
		}
		tr.TLSClientConfig = cfg
	case u.Scheme == "https":
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	case u.Scheme == "http":
	default:
		return nil, fmt.Errorf("server url must be https:// (or http:// to a loopback dev server), got %q", u.Scheme)
	}
	return &http.Client{Transport: tr, Timeout: requestTimeout}, nil
}

// response is one HTTP exchange's result. Status 0 means transport failure.
type response struct {
	Status     int
	Body       []byte
	RetryAfter time.Duration
}

// post sends body (already gzip-compressed when gzipped is set) as JSON.
func post(ctx context.Context, c *http.Client, endpoint, credential string, body []byte, gzipped bool) (response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return response{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "sentrix-agent/"+Version)
	if gzipped {
		req.Header.Set("Content-Encoding", "gzip")
	}
	if credential != "" {
		req.Header.Set("Authorization", wire.AuthScheme+" "+credential)
	}
	resp, err := c.Do(req)
	if err != nil {
		return response{}, err
	}
	defer func() { _ = resp.Body.Close() }() // response fully read or abandoned: nothing to lose
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return response{Status: resp.StatusCode}, fmt.Errorf("read response: %w", err)
	}
	return response{Status: resp.StatusCode, Body: data, RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}, nil
}

// retryAfter parses delay-seconds; HTTP-date and garbage mean "not given".
func retryAfter(v string) time.Duration {
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return min(time.Duration(n)*time.Second, 10*time.Minute)
}
