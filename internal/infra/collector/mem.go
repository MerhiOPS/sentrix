package collector

import (
	"context"

	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/shirou/gopsutil/v4/mem"
)

type memReader struct{}

func (memReader) name() string { return "memory" }

func (memReader) read(ctx context.Context, s *domain.CoreSample) error {
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return err
	}
	s.MemoryPercent = clampPercent(vm.UsedPercent)
	s.MemoryUsed = vm.Used
	return nil
}
