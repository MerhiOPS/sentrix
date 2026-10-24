package collector

import (
	"context"
	"errors"
	"strings"

	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/shirou/gopsutil/v4/disk"
)

// pseudoFS are filesystems that are not storage a user can fill.
var pseudoFS = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "squashfs": true, "overlay": true, "proc": true,
	"sysfs": true, "cgroup": true, "cgroup2": true, "devpts": true, "autofs": true,
	"mqueue": true, "tracefs": true, "debugfs": true, "securityfs": true, "pstore": true,
	"efivarfs": true, "bpf": true, "fusectl": true, "configfs": true, "ramfs": true, "nsfs": true,
}

// diskUsageReader reports the fullest real filesystem: the one that will
// cause the outage.
type diskUsageReader struct{}

func (diskUsageReader) name() string { return "disk usage" }

func (diskUsageReader) read(ctx context.Context, s *domain.CoreSample) error {
	parts, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return err
	}
	var firstErr error
	found := false
	for _, p := range parts {
		if pseudoFS[strings.ToLower(p.Fstype)] {
			continue
		}
		u, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if u.Total == 0 {
			continue
		}
		found = true
		if pct := clampPercent(u.UsedPercent); pct > s.DiskPercent {
			s.DiskPercent = pct
		}
	}
	if !found {
		return errors.Join(errors.New("no readable filesystem"), firstErr)
	}
	return nil
}

// diskIOReader sums read/write bytes over whole devices.
type diskIOReader struct{}

func (diskIOReader) name() string { return "disk io" }

func (diskIOReader) read(ctx context.Context, s *domain.CoreSample) error {
	io, err := disk.IOCountersWithContext(ctx)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(io))
	for n := range io {
		names = append(names, n)
	}
	for _, n := range wholeDevices(names) {
		s.DiskReadBytes += io[n].ReadBytes
		s.DiskWriteBytes += io[n].WriteBytes
	}
	return nil
}

// wholeDevices drops partitions and stacked devices so bytes are counted
// once: Linux lists sda and sda1, nvme0n1 and nvme0n1p1, and device-mapper
// volumes on top of both. A name is a partition when another listed device
// is its prefix and the rest is a partition suffix: digits (sda1) or "p" +
// digits (nvme0n1p1). So the 27th disk, sdaa, is not mistaken for sda's.
func wholeDevices(names []string) []string {
	present := make(map[string]bool, len(names))
	for _, n := range names {
		present[n] = true
	}
	var out []string
	for _, n := range names {
		if virtualBlock(n) || hasParentDevice(n, present) {
			continue
		}
		out = append(out, n)
	}
	return out
}

func virtualBlock(n string) bool {
	for _, p := range []string{"loop", "ram", "dm-", "zram", "md", "sr"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

func hasParentDevice(n string, present map[string]bool) bool {
	for i := len(n) - 1; i > 0; i-- {
		if present[n[:i]] && isPartitionSuffix(n[i:]) {
			return true
		}
	}
	return false
}

func isPartitionSuffix(s string) bool {
	s = strings.TrimPrefix(s, "p")
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
