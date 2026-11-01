package netguard

import (
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestPolicies(t *testing.T) {
	strict, private := Strict, Policy{AllowPrivate: true}
	cases := []struct {
		addr            string
		strict, private bool
	}{
		// Public: admitted by both.
		{"8.8.8.8", true, true}, {"1.1.1.1", true, true}, {"100.128.0.1", true, true},
		{"2606:4700:4700::1111", true, true},
		// Private: admitted only by AllowPrivate.
		{"127.0.0.1", false, true}, {"127.8.8.8", false, true}, {"::1", false, true},
		{"10.0.0.1", false, true}, {"172.16.5.5", false, true}, {"192.168.1.1", false, true},
		{"100.64.0.1", false, true}, {"100.127.255.254", false, true}, {"fc00::1", false, true}, {"fd12::1", false, true},
		// Never: metadata, link-local and special-purpose ranges.
		{"169.254.169.254", false, false}, {"169.254.0.1", false, false}, {"100.100.100.200", false, false},
		{"fd00:ec2::254", false, false}, {"fe80::1", false, false}, {"0.0.0.0", false, false}, {"0.1.2.3", false, false},
		{"224.0.0.1", false, false}, {"ff02::1", false, false}, {"255.255.255.255", false, false},
		{"240.0.0.1", false, false}, {"::", false, false}, {"::ffff:127.0.0.1", false, false},
		{"::ffff:8.8.8.8", false, false}, {"64:ff9b::7f00:1", false, false}, {"2002:7f00:1::", false, false},
		{"2001:0:4136:e378::1", false, false}, {"2001:db8::1", false, false}, {"198.18.0.1", false, false},
		{"fe80::1%eth0", false, false},
	}
	for _, c := range cases {
		a := netip.MustParseAddr(c.addr)
		if got := strict.Allowed(a); got != c.strict {
			t.Errorf("strict %s: %v, want %v", c.addr, got, c.strict)
		}
		if got := private.Allowed(a); got != c.private {
			t.Errorf("allow-private %s: %v, want %v", c.addr, got, c.private)
		}
	}
}

func TestControlRefusesBeforeConnecting(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	accepted := make(chan struct{}, 1)
	go func() {
		if c, err := ln.Accept(); err == nil {
			accepted <- struct{}{}
			_ = c.Close()
		}
	}()
	d := net.Dialer{Timeout: time.Second, Control: Strict.Control}
	if _, err := d.Dial("tcp", ln.Addr().String()); !errors.Is(err, ErrBlocked) {
		t.Fatalf("err %v, want ErrBlocked", err)
	}
	// Resolution happens before Control, so a name is checked by its address.
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	if _, err := d.Dial("tcp", net.JoinHostPort("localhost", port)); !errors.Is(err, ErrBlocked) {
		t.Fatalf("by name: err %v, want ErrBlocked", err)
	}
	select {
	case <-accepted:
		t.Fatal("a refused address was connected to")
	case <-time.After(100 * time.Millisecond):
	}
	allow := net.Dialer{Timeout: time.Second, Control: Policy{AllowPrivate: true}.Control}
	c, err := allow.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("allow-private refused loopback: %v", err)
	}
	_ = c.Close()
}

func TestControlRefusesUnparsableAddresses(t *testing.T) {
	for _, a := range []string{"no-port", "example.com:80"} {
		if err := Strict.Control("tcp", a, nil); !errors.Is(err, ErrBlocked) {
			t.Errorf("%q: err %v, want ErrBlocked", a, err)
		}
	}
}
