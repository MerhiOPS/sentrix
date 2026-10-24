// Package collector reads host metrics. One file per subsystem; platform
// differences behind build tags. Readers never fail the whole sample: a
// subsystem that cannot be read leaves its fields zero and reports why.
package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// reader fills its part of a CoreSample.
type reader interface {
	name() string
	read(ctx context.Context, s *domain.CoreSample) error
}

// Core collects CoreSamples.
type Core struct {
	readers []reader
	now     func() time.Time
}

// NewCore builds the standard set of readers.
func NewCore() *Core {
	return &Core{
		readers: []reader{cpuReader{}, memReader{}, diskUsageReader{}, diskIOReader{}, netReader{}, loadReader{}, uptimeReader{}},
		now:     time.Now,
	}
}

// Collect takes one sample. Errors are per subsystem and never prevent the
// sample: missing data is zero, and the caller logs what failed.
func (c *Core) Collect(ctx context.Context) (domain.CoreSample, []error) {
	s := domain.CoreSample{TS: c.now().UTC()}
	var errs []error
	for _, r := range c.readers {
		if err := r.read(ctx, &s); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.name(), err))
		}
	}
	return s, errs
}
