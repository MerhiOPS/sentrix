//go:build linux

package probe

import (
	"context"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// TestPingLoopback sends real echoes over an unprivileged ICMP socket. It
// fails rather than skips when the socket cannot be opened: CI must have
// ping_group_range set, as the packaged server needs.
func TestPingLoopback(t *testing.T) {
	if err := PingAvailable(); err != nil {
		t.Fatal(err)
	}
	m := domain.Monitor{Kind: domain.MonitorPing, Target: "127.0.0.1", Timeout: 3 * time.Second}
	r := loopback().Probe(context.Background(), m)
	if r.Outcome != domain.CheckUp || r.Loss != 0 || r.Latency < 0 {
		t.Fatalf("ping 127.0.0.1: %+v", r)
	}
	m.Target = "localhost"
	if r := loopback().Probe(context.Background(), m); r.Outcome != domain.CheckUp {
		t.Fatalf("ping localhost: %+v", r)
	}
}
