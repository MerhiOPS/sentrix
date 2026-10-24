package collector

import (
	"context"
	"strings"

	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/shirou/gopsutil/v4/net"
)

// netReader sums rx/tx bytes over physical-looking interfaces. Loopback and
// container/bridge interfaces are excluded: traffic between containers on
// one host would otherwise be counted twice (veth + bridge) and dwarf real
// network traffic.
type netReader struct{}

func (netReader) name() string { return "network" }

func (netReader) read(ctx context.Context, s *domain.CoreSample) error {
	ifs, err := net.IOCountersWithContext(ctx, true)
	if err != nil {
		return err
	}
	for _, i := range ifs {
		if virtualNIC(i.Name) {
			continue
		}
		s.NetRxBytes += i.BytesRecv
		s.NetTxBytes += i.BytesSent
	}
	return nil
}

func virtualNIC(name string) bool {
	n := strings.ToLower(name)
	if n == "lo" || strings.Contains(n, "loopback") {
		return true
	}
	for _, p := range []string{"veth", "docker", "br-", "virbr", "cni", "flannel", "cali", "tun", "tap"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}
