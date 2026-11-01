package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Echoes per ping check, and the gap between them.
const (
	PingCount = 3
	pingGap   = 200 * time.Millisecond
)

// pinger sends echoes to one address and returns the round trips of those
// answered (lost ones are absent).
type pinger interface {
	Ping(ctx context.Context, addr netip.Addr, count int) ([]time.Duration, error)
}

// pingHost resolves once, lets the policy decide on the resolved address,
// and pings exactly that address: there is no later resolution to rebind.
func (p *Prober) pingHost(ctx context.Context, m domain.Monitor) domain.CheckResult {
	addr, err := resolveOne(ctx, m.Target)
	if err != nil {
		return down(classify(err))
	}
	if !p.policy.Allowed(addr) {
		return down(domain.ErrBlocked)
	}
	rtts, err := p.ping.Ping(ctx, addr, PingCount)
	if err != nil && len(rtts) == 0 && !errors.Is(err, context.DeadlineExceeded) {
		return down(domain.ErrConnect)
	}
	r := domain.CheckResult{Loss: 100 * (PingCount - len(rtts)) / PingCount}
	if len(rtts) == 0 {
		r.Outcome, r.Class = domain.CheckDown, domain.ErrPingLoss
		return r
	}
	var sum time.Duration
	for _, d := range rtts {
		sum += d
	}
	r.Outcome, r.Latency = domain.CheckUp, sum/time.Duration(len(rtts))
	return r
}

// resolveOne returns the host's first address, IPv4 preferred.
func resolveOne(ctx context.Context, host string) (netip.Addr, error) {
	if a, err := netip.ParseAddr(host); err == nil {
		return a.Unmap(), nil
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return netip.Addr{}, err
	}
	for _, a := range addrs {
		if a.Unmap().Is4() {
			return a.Unmap(), nil
		}
	}
	if len(addrs) == 0 {
		return netip.Addr{}, &net.DNSError{Err: "no addresses", Name: host, IsNotFound: true}
	}
	return addrs[0], nil
}

// icmpPinger uses unprivileged ICMP sockets (SOCK_DGRAM, IPPROTO_ICMP):
// no capability, only net.ipv4.ping_group_range covering the process.
type icmpPinger struct{}

func (icmpPinger) Ping(ctx context.Context, addr netip.Addr, count int) ([]time.Duration, error) {
	network, listen, proto := "udp4", "0.0.0.0", 1
	var typ icmp.Type = ipv4.ICMPTypeEcho
	if addr.Is6() {
		network, listen, proto, typ = "udp6", "::", 58, ipv6.ICMPTypeEchoRequest
	}
	conn, err := icmp.ListenPacket(network, listen)
	if err != nil {
		return nil, fmt.Errorf("open ping socket: %w", err)
	}
	defer func() { _ = conn.Close() }()
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(time.Duration(count) * pingGap * 5)
	}
	_ = conn.SetDeadline(deadline)
	dst := &net.UDPAddr{IP: addr.AsSlice()}
	var rtts []time.Duration
	buf := make([]byte, 1500)
	for seq := 1; seq <= count; seq++ {
		msg := icmp.Message{Type: typ, Body: &icmp.Echo{ID: 0, Seq: seq, Data: []byte("sentrix-uptime")}}
		wire, err := msg.Marshal(nil)
		if err != nil {
			return rtts, err
		}
		sent := time.Now()
		if _, err := conn.WriteTo(wire, dst); err != nil {
			return rtts, err
		}
		limit := sent.Add(pingGap * 4)
		if deadline.Before(limit) {
			limit = deadline
		}
		if d, ok := awaitReply(conn, buf, proto, seq, addr, sent, limit); ok {
			rtts = append(rtts, d)
		}
		if ctx.Err() != nil {
			return rtts, ctx.Err()
		}
		if seq < count {
			time.Sleep(pingGap)
		}
	}
	return rtts, nil
}

// awaitReply reads until the echo reply for seq from addr arrives or limit
// passes. The kernel sets the echo id on unprivileged sockets, so replies
// are matched by sequence and source.
func awaitReply(conn *icmp.PacketConn, buf []byte, proto, seq int, addr netip.Addr, sent, limit time.Time) (time.Duration, bool) {
	_ = conn.SetReadDeadline(limit)
	for time.Now().Before(limit) {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			return 0, false
		}
		src, ok := from.(*net.UDPAddr)
		if !ok || netip.AddrFrom16([16]byte(src.IP.To16())).Unmap() != addr.Unmap() {
			continue
		}
		msg, err := icmp.ParseMessage(proto, buf[:n])
		if err != nil {
			continue
		}
		if echo, ok := msg.Body.(*icmp.Echo); ok && echo.Seq == seq &&
			(msg.Type == ipv4.ICMPTypeEchoReply || msg.Type == ipv6.ICMPTypeEchoReply) {
			return time.Since(sent), true
		}
	}
	return 0, false
}

// PingAvailable reports whether this process may open unprivileged ICMP
// sockets. Without them ping monitors are refused, never reported down.
func PingAvailable() error {
	conn, err := icmp.ListenPacket("udp4", "0.0.0.0")
	if err != nil {
		return fmt.Errorf("unprivileged ICMP sockets unavailable (net.ipv4.ping_group_range must include this process's group): %w", err)
	}
	return conn.Close()
}
