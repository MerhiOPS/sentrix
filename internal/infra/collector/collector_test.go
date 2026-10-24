package collector

import (
	"context"
	"math"
	"sort"
	"strings"
	"testing"
)

func TestWholeDevicesDropsPartitionsAndVirtual(t *testing.T) {
	got := wholeDevices([]string{"sda", "sda1", "sda2", "nvme0n1", "nvme0n1p1", "nvme0n1p2",
		"loop0", "dm-0", "zram0", "sr0", "vdb", "C:", "D:", "sdaa", "sdaa1"})
	sort.Strings(got)
	if strings.Join(got, ",") != "C:,D:,nvme0n1,sda,sdaa,vdb" {
		t.Fatalf("wholeDevices = %v", got)
	}
}

func TestVirtualNIC(t *testing.T) {
	for name, virtual := range map[string]bool{
		"lo": true, "Loopback Pseudo-Interface 1": true, "docker0": true, "veth12ab": true,
		"br-1a2b": true, "eth0": false, "enp3s0": false, "wlan0": false, "Ethernet": false,
	} {
		if got := virtualNIC(name); got != virtual {
			t.Errorf("virtualNIC(%q) = %v, want %v", name, got, virtual)
		}
	}
}

func TestClampPercent(t *testing.T) {
	for in, want := range map[float64]float64{-1: 0, 0: 0, 42.5: 42.5, 100.0000001: 100, math.Inf(1): 100} {
		if got := clampPercent(in); got != want {
			t.Errorf("clampPercent(%v) = %v, want %v", in, got, want)
		}
	}
	if clampPercent(math.NaN()) != 0 {
		t.Error("NaN not clamped to 0")
	}
}

// TestCollectOnThisMachine reads the real host. It must produce a sample the
// domain accepts — a collector producing invalid samples would lose every one.
func TestCollectOnThisMachine(t *testing.T) {
	c := NewCore()
	ctx := context.Background()
	_, _ = c.Collect(ctx) // prime CPU times
	s, errs := c.Collect(ctx)
	for _, e := range errs {
		t.Logf("subsystem error (non-fatal): %v", e)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("collected sample is invalid: %v (%+v)", err, s)
	}
	if s.MemoryUsed == 0 || s.UptimeSeconds == 0 {
		t.Fatalf("implausible sample: %+v", s)
	}
	if hi := ReadHostInfo(ctx); hi.Hostname == "" || hi.OS == "" {
		t.Fatalf("host info = %+v", hi)
	}
}
