package collector

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/shirou/gopsutil/v4/process"
)

// topN is how many processes each ranking keeps; the snapshot is the union
// of the top by CPU and the top by RSS (at most 2×topN entries).
const topN = 20

// Processes takes top-N snapshots. It keeps each process object between
// calls so CPU% is measured over the interval since the previous snapshot,
// not since the process started. Only names are read — never command lines,
// which carry secrets.
type Processes struct {
	known map[int32]*process.Process
	now   func() time.Time
}

// NewProcesses builds the snapshot collector.
func NewProcesses() *Processes {
	return &Processes{known: map[int32]*process.Process{}, now: time.Now}
}

// Collect returns one snapshot. Processes that vanish mid-read are skipped:
// a snapshot is best effort by nature.
func (p *Processes) Collect(ctx context.Context) (domain.ProcessSnapshot, error) {
	pids, err := process.PidsWithContext(ctx)
	if err != nil {
		return domain.ProcessSnapshot{}, err
	}
	seen := make(map[int32]bool, len(pids))
	var all []domain.Process
	for _, pid := range pids {
		seen[pid] = true
		proc, ok := p.known[pid]
		if !ok {
			if proc, err = process.NewProcessWithContext(ctx, pid); err != nil {
				continue
			}
			p.known[pid] = proc
		}
		if e, ok := readProcess(ctx, proc); ok {
			all = append(all, e)
		}
	}
	for pid := range p.known {
		if !seen[pid] {
			delete(p.known, pid)
		}
	}
	return domain.ProcessSnapshot{TS: p.now().UTC(), Procs: top(all)}, nil
}

func readProcess(ctx context.Context, proc *process.Process) (domain.Process, bool) {
	name, err := proc.NameWithContext(ctx)
	if err != nil || name == "" {
		return domain.Process{}, false
	}
	if len(name) > domain.MaxProcessNameLen {
		name = strings.ToValidUTF8(name[:domain.MaxProcessNameLen], "") // no half rune
	}
	cpuPct, _ := proc.PercentWithContext(ctx, 0)
	var rss uint64
	if mi, err := proc.MemoryInfoWithContext(ctx); err == nil && mi != nil {
		rss = mi.RSS
	}
	if cpuPct < 0 || cpuPct != cpuPct {
		cpuPct = 0
	}
	return domain.Process{Name: name, CPUPercent: cpuPct, RSS: rss}, true
}

// top returns the union of the topN by CPU and the topN by RSS.
func top(all []domain.Process) []domain.Process {
	byCPU := append([]domain.Process(nil), all...)
	sort.SliceStable(byCPU, func(i, j int) bool { return byCPU[i].CPUPercent > byCPU[j].CPUPercent })
	byRSS := append([]domain.Process(nil), all...)
	sort.SliceStable(byRSS, func(i, j int) bool { return byRSS[i].RSS > byRSS[j].RSS })
	type key struct {
		name string
		rss  uint64
		cpu  float64
	}
	in := map[key]bool{}
	var out []domain.Process
	for _, list := range [][]domain.Process{byCPU[:min(topN, len(byCPU))], byRSS[:min(topN, len(byRSS))]} {
		for _, p := range list {
			k := key{p.Name, p.RSS, p.CPUPercent}
			if !in[k] {
				in[k] = true
				out = append(out, p)
			}
		}
	}
	return out
}
