// Package netguard decides which addresses the server may connect to on a
// tenant's behalf (alert webhooks, uptime checks). It is the SSRF boundary:
// a tenant-supplied URL must never reach the server's own network, its
// loopback services or a cloud metadata endpoint.
package netguard

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"syscall"
)

// ErrBlocked is returned when a destination resolves to a refused address.
var ErrBlocked = errors.New("destination address not allowed")

// Policy is which address ranges are admitted.
type Policy struct {
	// AllowPrivate admits loopback, RFC 1918, IPv6 ULA and CGNAT: for a
	// single-company install monitoring its own network. Cloud metadata,
	// link-local and the special-purpose ranges stay refused.
	AllowPrivate bool
}

// Strict refuses every non-public address. Webhooks always use it.
var Strict = Policy{}

// alwaysBlocked is refused under every policy.
var alwaysBlocked = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),          // "this network"
	netip.MustParsePrefix("169.254.0.0/16"),     // link-local, cloud metadata (also IsLinkLocalUnicast)
	netip.MustParsePrefix("100.100.100.200/32"), // Alibaba Cloud metadata, inside CGNAT space
	netip.MustParsePrefix("fd00:ec2::254/128"),  // AWS IPv6 metadata, inside ULA space
	netip.MustParsePrefix("192.0.0.0/24"),       // IETF protocol assignments
	netip.MustParsePrefix("198.18.0.0/15"),      // benchmarking
	netip.MustParsePrefix("240.0.0.0/4"),        // reserved, incl. broadcast
	netip.MustParsePrefix("64:ff9b::/96"),       // NAT64: maps onto IPv4 space
	netip.MustParsePrefix("64:ff9b:1::/48"),     // local-use NAT64
	netip.MustParsePrefix("2002::/16"),          // 6to4: embeds an IPv4 address
	netip.MustParsePrefix("2001:db8::/32"),      // documentation
	netip.MustParsePrefix("fec0::/10"),          // deprecated site-local
	netip.MustParsePrefix("100::/64"),           // discard-only
	netip.MustParsePrefix("2001::/32"),          // Teredo: embeds an IPv4 address
	netip.MustParsePrefix("::/128"),             // unspecified (also IsUnspecified)
	netip.MustParsePrefix("255.255.255.255/32"),
}

// privateRanges are refused unless the policy allows private targets.
var privateRanges = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT: provider-internal
}

// Allowed reports whether the server may connect to addr. Always refused:
// link-local (169.254.169.254, the cloud metadata service, included) and the
// other metadata addresses, 0.0.0.0/8, multicast, reserved and documentation
// ranges, the IPv6 transition ranges that embed an IPv4 address, zoned
// addresses and IPv4-mapped IPv6 (which would smuggle any IPv4 address past
// an IPv6-only check). Refused unless AllowPrivate: loopback, RFC 1918, IPv6
// ULA and CGNAT.
func (p Policy) Allowed(addr netip.Addr) bool {
	if !addr.IsValid() || addr.Is4In6() || addr.Zone() != "" {
		return false
	}
	if addr.IsUnspecified() || addr.IsMulticast() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() || inAny(addr, alwaysBlocked) {
		return false
	}
	if p.AllowPrivate {
		return true
	}
	return !addr.IsLoopback() && !addr.IsPrivate() && !inAny(addr, privateRanges)
}

func inAny(addr netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// Control is a net.Dialer Control hook. It runs after resolution, on the
// exact address being connected to, so DNS rebinding (a name resolving to a
// public address at check time and 127.0.0.1 at connect time) cannot pass:
// there is no separate check time. It runs before the connection is opened,
// so a refused address sees no packet.
func (p Policy) Control(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBlocked, err)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || !p.Allowed(addr) {
		return fmt.Errorf("%w: %s", ErrBlocked, host)
	}
	return nil
}
