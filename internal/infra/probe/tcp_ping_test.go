package probe

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/netguard"
)

func TestTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	open := ln.Addr().String()
	closedLn, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := closedLn.Addr().String()
	_ = closedLn.Close()
	defer func() { _ = ln.Close() }()
	m := domain.Monitor{Kind: domain.MonitorTCP, Timeout: time.Second}
	m.Target = open
	// A loopback connect can measure 0 at the clock's resolution (Windows).
	if r := loopback().Probe(context.Background(), m); r.Outcome != domain.CheckUp || r.Latency < 0 {
		t.Fatalf("open port: %+v", r)
	}
	m.Target = closed
	expect(t, loopback().Probe(context.Background(), m), domain.CheckDown, domain.ErrConnect)
	m.Target = open
	expect(t, New(netguard.Strict).Probe(context.Background(), m), domain.CheckDown, domain.ErrBlocked)
}

// fakePinger answers some echoes.
type fakePinger struct {
	answered int
	err      error
	got      netip.Addr
}

func (f *fakePinger) Ping(_ context.Context, addr netip.Addr, count int) ([]time.Duration, error) {
	f.got = addr
	var out []time.Duration
	for range min(f.answered, count) {
		out = append(out, 10*time.Millisecond)
	}
	return out, f.err
}

func TestPingOutcomes(t *testing.T) {
	m := domain.Monitor{Kind: domain.MonitorPing, Target: "127.0.0.1", Timeout: time.Second}
	cases := []struct {
		f        fakePinger
		outcome  domain.CheckOutcome
		class    domain.ErrorClass
		loss     int
		probeVia *Prober
	}{
		{fakePinger{answered: 3}, domain.CheckUp, "", 0, loopback()},
		{fakePinger{answered: 2}, domain.CheckUp, "", 33, loopback()},
		{fakePinger{answered: 0}, domain.CheckDown, domain.ErrPingLoss, 100, loopback()},
		{fakePinger{err: errors.New("socket: operation not permitted")}, domain.CheckDown, domain.ErrConnect, 0, loopback()},
		{fakePinger{answered: 3}, domain.CheckDown, domain.ErrBlocked, 0, New(netguard.Strict)},
	}
	for i, c := range cases {
		p := c.probeVia
		f := c.f
		p.ping = &f
		r := p.Probe(context.Background(), m)
		if r.Outcome != c.outcome || r.Class != c.class || r.Loss != c.loss {
			t.Errorf("case %d: %+v, want %s/%q loss %d", i, r, c.outcome, c.class, c.loss)
		}
		if c.class == domain.ErrBlocked && f.got.IsValid() {
			t.Errorf("case %d: a blocked address was pinged", i)
		}
	}
	p := New(netguard.Policy{AllowPrivate: true})
	f := &fakePinger{answered: 3}
	p.ping = f
	expect(t, p.Probe(context.Background(), domain.Monitor{Kind: domain.MonitorPing, Target: "sentrix-ping.invalid", Timeout: time.Second}),
		domain.CheckDown, domain.ErrDNS)
	if f.got.IsValid() {
		t.Fatal("an unresolvable name was pinged")
	}
}
