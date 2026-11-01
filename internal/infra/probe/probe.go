// Package probe runs uptime checks: HTTP(S), TCP and ICMP ping. Every
// connection is dialled through the netguard policy, so a monitor can never
// reach an address the operator has not admitted, and failures are reported
// as error classes only.
package probe

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/netguard"
)

// Prober checks monitors.
type Prober struct {
	policy  netguard.Policy
	control func(network, address string, c syscall.RawConn) error
	roots   *x509.CertPool // nil: the system roots
	now     func() time.Time
	ping    pinger
}

// New returns a prober admitting the addresses policy allows.
func New(policy netguard.Policy) *Prober {
	return &Prober{policy: policy, control: policy.Control, now: time.Now, ping: icmpPinger{}}
}

// Probe runs one check of m, bounded by m.Timeout. It never returns an
// error: every failure is a down result with its class.
func (p *Prober) Probe(ctx context.Context, m domain.Monitor) domain.CheckResult {
	ctx, cancel := context.WithTimeout(ctx, m.Timeout)
	defer cancel()
	start := p.now()
	var r domain.CheckResult
	switch m.Kind {
	case domain.MonitorHTTP:
		r = p.http(ctx, m)
	case domain.MonitorTCP:
		r = p.tcp(ctx, m)
	case domain.MonitorPing:
		r = p.pingHost(ctx, m)
	default:
		r = down(domain.ErrConnect)
	}
	r.At = start
	if r.Outcome == domain.CheckDown && r.Class == domain.ErrNone {
		r.Class = domain.ErrConnect
	}
	return r
}

func down(c domain.ErrorClass) domain.CheckResult {
	return domain.CheckResult{Outcome: domain.CheckDown, Class: c}
}

// classify maps a transport error to its class. The error itself is
// dropped: it can name internal addresses, and only the class is stored.
func classify(err error) domain.ErrorClass {
	var dnsErr *net.DNSError
	switch {
	case errors.Is(err, netguard.ErrBlocked):
		return domain.ErrBlocked
	case errors.As(err, &dnsErr):
		if dnsErr.IsTimeout {
			return domain.ErrTimeout
		}
		return domain.ErrDNS
	case isTLS(err):
		return domain.ErrTLS
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) || isTimeout(err):
		return domain.ErrTimeout
	default:
		return domain.ErrConnect
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
