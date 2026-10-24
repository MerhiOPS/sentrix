package collector

import (
	"context"

	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/shirou/gopsutil/v4/host"
)

type uptimeReader struct{}

func (uptimeReader) name() string { return "uptime" }

func (uptimeReader) read(ctx context.Context, s *domain.CoreSample) error {
	up, err := host.UptimeWithContext(ctx)
	if err != nil {
		return err
	}
	s.UptimeSeconds = up
	return nil
}
