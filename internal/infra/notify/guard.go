// Package notify delivers alert notifications to webhooks and Slack through
// an HTTP client that refuses to reach internal addresses (SSRF guard).
package notify

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"syscall"
)

// ErrBlocked is returned when a destination resolves to a refused address.
var ErrBlocked = errors.New("destination address not allowed")

// blockedPrefixes are refused on top of what netip's predicates cover.
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),      // "this network"
	netip.MustParsePrefix("100.64.0.0/10"),  // CGNAT: provider-internal
	netip.MustParsePrefix("192.0.0.0/24"),   // IETF protocol assignments
	netip.MustParsePrefix("198.18.0.0/15"),  // benchmarking
	netip.MustParsePrefix("240.0.0.0/4"),    // reserved, incl. broadcast
	netip.MustParsePrefix("64:ff9b::/96"),   // NAT64: maps onto IPv4 space
	netip.MustParsePrefix("64:ff9b:1::/48"), // local-use NAT64
	netip.MustParsePrefix("2002::/16"),      // 6to4: embeds an IPv4 address
	netip.MustParsePrefix("2001:db8::/32"),  // documentation
	netip.MustParsePrefix("fec0::/10"),      // deprecated site-local
	netip.MustParsePrefix("100::/64"),       // discard-only
	netip.MustParsePrefix("2001::/32"),      // Teredo: embeds an IPv4 address
	netip.MustParsePrefix("::/128"),         // unspecified (also IsUnspecified)
	netip.MustParsePrefix("169.254.0.0/16"), // link-local, cloud metadata (also IsLinkLocalUnicast)
	netip.MustParsePrefix("255.255.255.255/32"),
}

// Allowed reports whether a notification may be sent to addr. It is the
// deny list from PLAN Phase 5: loopback, link-local (169.254.169.254, the
// cloud metadata service, included), RFC 1918 and IPv6 ULA, CGNAT,
// 0.0.0.0/8, multicast, and IPv4-mapped IPv6 (which would smuggle any IPv4
// address past an IPv6-only check).
func Allowed(addr netip.Addr) bool {
	if !addr.IsValid() || addr.Is4In6() || addr.Zone() != "" {
		return false
	}
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsUnspecified() || addr.IsMulticast() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsInterfaceLocalMulticast() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

// control is the dialer's Control hook. It runs after resolution, on the
// exact address being connected to, so DNS rebinding (a name resolving to a
// public address at check time and 127.0.0.1 at connect time) cannot pass:
// there is no separate check time.
func control(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBlocked, err)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || !Allowed(addr) {
		return fmt.Errorf("%w: %s", ErrBlocked, host)
	}
	return nil
}
