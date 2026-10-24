//go:build windows

package collector

import (
	"context"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// loadReader reports zero on Windows, which has no load average. gopsutil
// emulates one from the processor queue, but only after a background
// sampler has run for minutes; reporting a fabricated value would be worse
// than reporting none (the UI hides load for Windows hosts).
type loadReader struct{}

func (loadReader) name() string { return "load" }

func (loadReader) read(context.Context, *domain.CoreSample) error { return nil }
