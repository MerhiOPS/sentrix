package tlscert

import (
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
)

// ErrPinMismatch means the server presented a key other than the pinned one.
// The agent treats it as fatal, never as a warning (PLAN Phase 6).
var ErrPinMismatch = errors.New("server key does not match the pinned key")

// PinnedClientConfig returns a client TLS config that accepts exactly the
// server key with this SPKI pin. Chain and name verification are replaced
// by the pin — the self-signed certificate has neither a CA nor the host's
// real name — so the pin check is the only thing standing in for them and
// runs on every handshake, including resumed ones.
func PinnedClientConfig(pin []byte) (*tls.Config, error) {
	if len(pin) != 32 {
		return nil, fmt.Errorf("pin must be 32 bytes, got %d", len(pin))
	}
	want := append([]byte(nil), pin...)
	return &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true, //nolint:gosec // replaced by VerifyConnection's SPKI pin check
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return ErrPinMismatch
			}
			return checkPin(cs.PeerCertificates[0], want)
		},
	}, nil
}

func checkPin(leaf *x509.Certificate, want []byte) error {
	if subtle.ConstantTimeCompare(SPKIPin(leaf), want) != 1 {
		return ErrPinMismatch
	}
	return nil
}

// ServerConfig is the server side: TLS 1.3 with the given certificate.
func ServerConfig(c tls.Certificate) *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{c}}
}
