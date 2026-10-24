package main

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/logtail"
)

// Simulated collectors for --demo. They stand in for the host, not for the
// agent: the real agent code batches, buffers and ships what they produce.

type simCore struct {
	mu                sync.Mutex
	phase, base, peak float64
	rx, tx, dr, dw    uint64
	memTotal          uint64
}

func newSimCore(i int) *simCore {
	base, peak := 35.0, 25.0
	if i == 0 {
		base, peak = 50, 44 // web-01 passes 85 % every few minutes: the demo alert fires
	}
	return &simCore{phase: float64(i) * 1.7, base: base, peak: peak, memTotal: 8 << 30}
}

func wave(period time.Duration, phase float64) float64 {
	return math.Sin(2*math.Pi*float64(time.Now().UnixNano())/float64(period) + phase)
}

func (c *simCore) Collect(context.Context) (domain.CoreSample, []error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cpu := clamp(c.base+c.peak*wave(6*time.Minute, c.phase)+rand.NormFloat64()*3, 1, 99) //nolint:gosec // simulated noise, not a secret
	mem := clamp(48+8*wave(40*time.Minute, c.phase), 5, 95)
	c.rx += uint64(200_000 + 150_000*(1+wave(3*time.Minute, c.phase)))
	c.tx += uint64(80_000 + 60_000*(1+wave(3*time.Minute, c.phase+1)))
	c.dr += uint64(50_000 + rand.IntN(40_000))  //nolint:gosec // simulated noise, not a secret
	c.dw += uint64(120_000 + rand.IntN(80_000)) //nolint:gosec // simulated noise, not a secret
	return domain.CoreSample{TS: time.Now(), CPUPercent: cpu, MemoryPercent: mem, DiskPercent: 61 + 3*wave(time.Hour, c.phase),
		Load1: cpu / 25, Load5: cpu / 28, Load15: cpu / 30, MemoryUsed: uint64(mem / 100 * float64(c.memTotal)),
		UptimeSeconds: uint64(time.Since(demoStart).Seconds()) + 86_400*3,
		NetRxBytes:    c.rx, NetTxBytes: c.tx, DiskReadBytes: c.dr, DiskWriteBytes: c.dw}, nil
}

var demoStart = time.Now()

type simDetail struct {
	core *simCore
	nic  uint64
}

func (d *simDetail) Collect(context.Context) ([]domain.DetailSample, []error) {
	now := time.Now()
	var out []domain.DetailSample
	for i := range 4 {
		v := clamp(d.core.base+d.core.peak*wave(6*time.Minute, d.core.phase+float64(i)*0.3)+rand.NormFloat64()*8, 0, 100)
		out = append(out, domain.DetailSample{TS: now, Metric: domain.MetricCPUCore, Kind: domain.KindGauge,
			Labels: domain.Labels{"core": fmt.Sprint(i)}, Value: v})
	}
	for _, m := range []struct {
		mount string
		used  float64
	}{{"/", 61}, {"/var", 34}} {
		out = append(out, domain.DetailSample{TS: now, Metric: domain.MetricDiskUsed, Kind: domain.KindGauge,
			Labels: domain.Labels{"mount": m.mount}, Value: m.used})
	}
	d.nic += 250_000
	out = append(out, domain.DetailSample{TS: now, Metric: domain.MetricNetRx, Kind: domain.KindCounter,
		Labels: domain.Labels{"nic": "eth0"}, Value: float64(d.nic)})
	return out, nil
}

type simProcs struct{}

func (simProcs) Collect(context.Context) (domain.ProcessSnapshot, error) {
	procs := []domain.Process{}
	for _, p := range []struct {
		name string
		cpu  float64
		rss  uint64
	}{{"nginx", 12, 90 << 20}, {"postgres", 25, 1200 << 20}, {"node", 18, 450 << 20}, {"sentrix-agent", 0.2, 14 << 20}, {"sshd", 0.1, 8 << 20}} {
		procs = append(procs, domain.Process{Name: p.name, CPUPercent: p.cpu * (0.5 + rand.Float64()), RSS: p.rss})
	}
	return domain.ProcessSnapshot{TS: time.Now(), Procs: procs}, nil
}

// simLogs emits application log lines, some carrying values the server's
// redaction must remove before storage (an email, an address, a token).
type simLogs struct {
	host string
	n    int
}

var demoLines = []struct {
	level domain.Level
	msg   string
}{
	{domain.LevelInfo, "GET /api/orders 200 in %d ms"},
	{domain.LevelInfo, "user login ok: email=alice@example.com from 203.0.113.%d"},
	{domain.LevelWarn, "slow query on orders (%d ms), consider an index"},
	{domain.LevelInfo, "POST /api/checkout 201 in %d ms"},
	{domain.LevelError, "payment gateway refused request %d: Authorization: Bearer sk_demo_51HxQ2eFakeTokenValue"},
	{domain.LevelInfo, "worker processed %d jobs from the queue"},
}

func (s *simLogs) Name() string { return "demo-app" }

func (s *simLogs) Poll(context.Context) ([]domain.LogLine, logtail.Mark, error) {
	if rand.IntN(3) != 0 {
		return nil, nil, nil
	}
	s.n++
	l := demoLines[s.n%len(demoLines)]
	return []domain.LogLine{{TS: time.Now(), Source: "/var/log/app/" + s.host + ".log", Level: l.level,
		Message: fmt.Sprintf(l.msg, 5+rand.IntN(400))}}, s.n, nil
}

func (s *simLogs) Commit(m logtail.Mark) ([]byte, error) { return []byte(fmt.Sprint(m)), nil }
func (s *simLogs) Restore([]byte) error                  { return nil }
func (s *simLogs) Close() error                          { return nil }

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
