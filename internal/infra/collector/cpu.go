package collector

import (
	"context"
	"errors"

	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/shirou/gopsutil/v4/cpu"
)

// cpuReader reports total CPU utilisation since the previous call. gopsutil
// keeps the previous CPU times when the interval is 0, so consecutive calls
// measure exactly one sampling interval without sleeping.
type cpuReader struct{}

func (cpuReader) name() string { return "cpu" }

func (cpuReader) read(ctx context.Context, s *domain.CoreSample) error {
	pct, err := cpu.PercentWithContext(ctx, 0, false)
	if err != nil {
		return err
	}
	if len(pct) == 0 {
		return errors.New("no cpu reading")
	}
	s.CPUPercent = clampPercent(pct[0])
	return nil
}

// clampPercent bounds OS-reported percentages to [0, 100]: rounding in the
// kernel accounting occasionally reports 100.0000001, which would fail
// domain validation and lose the whole sample.
func clampPercent(v float64) float64 {
	switch {
	case v < 0 || v != v: // negative or NaN
		return 0
	case v > 100:
		return 100
	default:
		return v
	}
}
