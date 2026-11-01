package probe

import (
	"context"
	"net"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// tcp is up when a connection to host:port opens within the timeout; the
// latency is the time to connect.
func (p *Prober) tcp(ctx context.Context, m domain.Monitor) domain.CheckResult {
	start := time.Now()
	c, err := (&net.Dialer{Control: p.control}).DialContext(ctx, "tcp", m.Target)
	if err != nil {
		return down(classify(err))
	}
	latency := time.Since(start)
	_ = c.Close()
	return domain.CheckResult{Outcome: domain.CheckUp, Latency: latency}
}
