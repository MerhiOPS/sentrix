package collector

import (
	"context"
	"os"
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v4/host"
)

// HostInfo is what the agent reports at enrollment.
type HostInfo struct {
	Hostname string
	OS       string
	Platform string
}

// ReadHostInfo describes this machine. A failure to read the platform is
// not fatal: it is informational.
func ReadHostInfo(ctx context.Context) HostInfo {
	hi := HostInfo{OS: runtime.GOOS}
	if info, err := host.InfoWithContext(ctx); err == nil {
		hi.Hostname = info.Hostname
		hi.Platform = strings.TrimSpace(info.Platform + " " + info.PlatformVersion)
	}
	if hi.Hostname == "" {
		hi.Hostname, _ = os.Hostname()
	}
	return hi
}
