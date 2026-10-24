//go:build !windows

package collector

import (
	"context"

	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/shirou/gopsutil/v4/load"
)

type loadReader struct{}

func (loadReader) name() string { return "load" }

func (loadReader) read(ctx context.Context, s *domain.CoreSample) error {
	a, err := load.AvgWithContext(ctx)
	if err != nil {
		return err
	}
	s.Load1, s.Load5, s.Load15 = nonNegative(a.Load1), nonNegative(a.Load5), nonNegative(a.Load15)
	return nil
}

func nonNegative(v float64) float64 {
	if v < 0 || v != v {
		return 0
	}
	return v
}
