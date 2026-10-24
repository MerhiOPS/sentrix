package collector

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/net"
)

// detailReader appends one subsystem's detail samples.
type detailReader interface {
	name() string
	readDetail(ctx context.Context, ts time.Time) ([]domain.DetailSample, error)
}

// Detail collects the per-core / per-mount / per-device / per-NIC series.
// It reuses the core readers' filters (pseudo filesystems, partitions,
// virtual NICs) so the detail view and the totals agree.
type Detail struct {
	readers []detailReader
	now     func() time.Time
}

// NewDetail builds the standard detail readers.
func NewDetail() *Detail {
	return &Detail{readers: []detailReader{coresReader{}, mountsReader{}, devicesReader{}, nicsReader{}}, now: time.Now}
}

// Collect takes one round of detail samples; failures are per subsystem.
func (d *Detail) Collect(ctx context.Context) ([]domain.DetailSample, []error) {
	ts := d.now().UTC()
	var out []domain.DetailSample
	var errs []error
	for _, r := range d.readers {
		s, err := r.readDetail(ctx, ts)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.name(), err))
		}
		out = append(out, s...)
	}
	return out, errs
}

func gauge(ts time.Time, metric, label, value string, v float64) domain.DetailSample {
	return domain.DetailSample{TS: ts, Metric: metric, Kind: domain.KindGauge, Labels: domain.Labels{label: value}, Value: v}
}

func counter(ts time.Time, metric, label, value string, v uint64) domain.DetailSample {
	return domain.DetailSample{TS: ts, Metric: metric, Kind: domain.KindCounter, Labels: domain.Labels{label: value}, Value: float64(v)}
}

type coresReader struct{}

func (coresReader) name() string { return "cpu cores" }

func (coresReader) readDetail(ctx context.Context, ts time.Time) ([]domain.DetailSample, error) {
	pct, err := cpu.PercentWithContext(ctx, 0, true)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DetailSample, 0, len(pct))
	for i, p := range pct {
		out = append(out, gauge(ts, domain.MetricCPUCore, "core", strconv.Itoa(i), clampPercent(p)))
	}
	return out, nil
}

type mountsReader struct{}

func (mountsReader) name() string { return "mounts" }

func (mountsReader) readDetail(ctx context.Context, ts time.Time) ([]domain.DetailSample, error) {
	parts, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return nil, err
	}
	var out []domain.DetailSample
	var errs []error
	for _, p := range parts {
		if pseudoFS[strings.ToLower(p.Fstype)] {
			continue
		}
		u, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if u.Total > 0 {
			out = append(out, gauge(ts, domain.MetricDiskUsed, "mount", p.Mountpoint, clampPercent(u.UsedPercent)))
		}
	}
	return out, errors.Join(errs...)
}

type devicesReader struct{}

func (devicesReader) name() string { return "disk devices" }

func (devicesReader) readDetail(ctx context.Context, ts time.Time) ([]domain.DetailSample, error) {
	io, err := disk.IOCountersWithContext(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(io))
	for n := range io {
		names = append(names, n)
	}
	var out []domain.DetailSample
	for _, n := range wholeDevices(names) {
		out = append(out, counter(ts, domain.MetricDiskRead, "device", n, io[n].ReadBytes),
			counter(ts, domain.MetricDiskWrite, "device", n, io[n].WriteBytes))
	}
	return out, nil
}

type nicsReader struct{}

func (nicsReader) name() string { return "nics" }

func (nicsReader) readDetail(ctx context.Context, ts time.Time) ([]domain.DetailSample, error) {
	ifs, err := net.IOCountersWithContext(ctx, true)
	if err != nil {
		return nil, err
	}
	var out []domain.DetailSample
	for _, i := range ifs {
		if virtualNIC(i.Name) {
			continue
		}
		out = append(out, counter(ts, domain.MetricNetRx, "nic", i.Name, i.BytesRecv),
			counter(ts, domain.MetricNetTx, "nic", i.Name, i.BytesSent))
	}
	return out, nil
}
