package ingesthttp

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Server timeouts. The ingest port faces the internet (OLD B15): a client
// that dribbles headers or holds idle connections must not pin goroutines.
// There is no WriteTimeout — a slow but honest agent ack should not be cut.
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	idleTimeout       = 120 * time.Second
)

// NewServer builds the http.Server for the ingest endpoint. tlsCfg nil means
// plain HTTP, which CheckListen permits on loopback only.
func NewServer(addr string, h http.Handler, tlsCfg *tls.Config) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    16 << 10,
	}
}

// ErrInsecurePublic: plain HTTP was requested on a non-loopback address.
var ErrInsecurePublic = errors.New("--insecure only binds loopback addresses")

// CheckListen refuses plain HTTP on anything but loopback (PLAN §2):
// credentials and log content would cross the network in the clear.
// An empty host (":8443") means every interface, and is refused too.
func CheckListen(addr string, insecure bool) error {
	if !insecure {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("listen address %q: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%w: %q", ErrInsecurePublic, addr)
	}
	return nil
}
