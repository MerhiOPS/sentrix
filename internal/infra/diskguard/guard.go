// Package diskguard implements the log disk watermark: below a free-space
// ratio on the data directory, logs are shed and metrics keep flowing
// (PLAN Phase 4). Logs are the unbounded input; metrics are not.
package diskguard

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
)

// Guard checks free space at most once per interval.
type Guard struct {
	path     string
	minFree  float64 // e.g. 0.20
	interval time.Duration
	usage    func(path string) (free, total uint64, err error)
	now      func() time.Time

	mu      sync.Mutex
	checked time.Time
	allowed bool
}

// New watches path, allowing logs while free/total >= minFree.
func New(path string, minFree float64, interval time.Duration) *Guard {
	return &Guard{path: path, minFree: minFree, interval: interval, usage: diskUsage, now: time.Now, allowed: true}
}

func diskUsage(path string) (uint64, uint64, error) {
	u, err := disk.Usage(path)
	if err != nil {
		return 0, 0, err
	}
	return u.Free, u.Total, nil
}

// LogsAllowed implements ingest.DiskGuard. A failed check keeps the last
// answer: a transient statfs error must neither drop logs nor fill the disk.
func (g *Guard) LogsAllowed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	if now.Sub(g.checked) < g.interval && !g.checked.IsZero() {
		return g.allowed
	}
	g.checked = now
	free, total, err := g.usage(g.path)
	if err != nil || total == 0 {
		slog.Warn("disk guard: cannot read free space", "path", g.path, "err", err)
		return g.allowed
	}
	allowed := float64(free)/float64(total) >= g.minFree
	if allowed != g.allowed {
		slog.Warn("disk guard: log ingest "+map[bool]string{true: "resumed", false: "shedding"}[allowed],
			"free_ratio", float64(free)/float64(total))
	}
	g.allowed = allowed
	return allowed
}

// FreeRatio reads free/total now (for the system report).
func (g *Guard) FreeRatio() (float64, error) {
	free, total, err := g.usage(g.path)
	if err != nil || total == 0 {
		return 0, fmt.Errorf("read free space: %w", errors.Join(err, errNoTotal(total)))
	}
	return float64(free) / float64(total), nil
}

func errNoTotal(total uint64) error {
	if total == 0 {
		return errors.New("filesystem reports no size")
	}
	return nil
}
