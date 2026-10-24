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
	period            time.Duration
	rx, tx, dr, dw    uint64
	memTotal          uint64
}

func newSimCore(i int) *simCore {
	base, peak, period := 35.0, 25.0, 6*time.Minute
	if i == 0 {
		// web-01 stays above 80 % for ~3 minutes of every 10: the demo
		// alert fires and resolves while you watch.
		base, peak, period = 52, 44, 10*time.Minute
	}
	return &simCore{phase: float64(i) * 1.7, base: base, peak: peak, period: period, memTotal: 8 << 30}
}

func wave(period time.Duration, phase float64) float64 {
	return math.Sin(2*math.Pi*float64(time.Now().UnixNano())/float64(period) + phase)
}

func (c *simCore) Collect(context.Context) (domain.CoreSample, []error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cpu := clamp(c.base+c.peak*wave(c.period, c.phase)+noise()*3, 1, 99)
	mem := clamp(48+8*wave(40*time.Minute, c.phase), 5, 95)
	c.rx += uint64(200_000 + 150_000*(1+wave(3*time.Minute, c.phase)))
	c.tx += uint64(80_000 + 60_000*(1+wave(3*time.Minute, c.phase+1)))
	c.dr += 50_000 + simUint(40_000)
	c.dw += 120_000 + simUint(80_000)
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
		v := clamp(d.core.base+d.core.peak*wave(d.core.period, d.core.phase+float64(i)*0.3)+noise()*8, 0, 100)
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
		procs = append(procs, domain.Process{Name: p.name, CPUPercent: p.cpu * (0.5 + simUnit()), RSS: p.rss})
	}
	return domain.ProcessSnapshot{TS: time.Now(), Procs: procs}, nil
}

// simLogs emits application log lines, some carrying values the server's
// redaction must remove before storage (an email, an address, a token).
type simLogs struct {
	host string
	n    int
}

// Each line's number is drawn from its own range (max): an address octet
// must stay a valid octet, or the redactor rightly leaves it alone.
var demoLines = []struct {
	level domain.Level
	msg   string
	max   int
}{
	{domain.LevelInfo, "GET /api/orders 200 in %d ms", 400},
	{domain.LevelInfo, "user login ok: email=alice@example.com from 203.0.113.%d", 254},
	{domain.LevelWarn, "slow query on orders (%d ms), consider an index", 900},
	{domain.LevelInfo, "POST /api/checkout 201 in %d ms", 400},
	{domain.LevelError, "payment gateway refused request %d: Authorization: Bearer sk_demo_51HxQ2eFakeTokenValue", 9999},
	{domain.LevelInfo, "worker processed %d jobs from the queue", 500},
}

func (s *simLogs) Name() string { return "demo-app" }

func (s *simLogs) Poll(context.Context) ([]domain.LogLine, logtail.Mark, error) {
	if simInt(3) != 0 {
		return nil, nil, nil
	}
	s.n++
	l := demoLines[s.n%len(demoLines)]
	return []domain.LogLine{{TS: time.Now(), Source: "/var/log/app/" + s.host + ".log", Level: l.level,
		Message: fmt.Sprintf(l.msg, 1+simInt(l.max))}}, s.n, nil
}

func (s *simLogs) Commit(m logtail.Mark) ([]byte, error) { return []byte(fmt.Sprint(m)), nil }
func (s *simLogs) Restore([]byte) error                  { return nil }
func (s *simLogs) Close() error                          { return nil }

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

// The simulation's randomness: shapes for charts, never a secret.
func noise() float64          { return rand.NormFloat64() } //nolint:gosec // simulated metric noise
func simInt(n int) int        { return rand.IntN(n) }       //nolint:gosec // simulated metric noise
func simUnit() float64        { return rand.Float64() }     //nolint:gosec // simulated metric noise
func simUint(n uint64) uint64 { return rand.Uint64N(n) }    //nolint:gosec // simulated metric noise
