package probe

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"strings"
)

// isTLS reports a certificate or handshake failure: an expired, untrusted
// or wrong-name certificate, or a server that does not speak TLS.
func isTLS(err error) bool {
	var verify *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var host x509.HostnameError
	var record tls.RecordHeaderError
	var alert tls.AlertError
	if errors.As(err, &verify) || errors.As(err, &unknown) || errors.As(err, &invalid) || errors.As(err, &host) ||
		errors.As(err, &record) || errors.As(err, &alert) {
		return true
	}
	// The handshake's own failures are plain errors with a stable prefix,
	// and net/http reports a plain-HTTP server behind an https URL in words.
	return err != nil && (strings.Contains(err.Error(), "tls: ") ||
		strings.Contains(err.Error(), "server gave HTTP response to HTTPS client"))
}
