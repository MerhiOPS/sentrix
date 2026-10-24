package memstore

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// CommitBatch implements ingest.Store. The whole call holds the lock, and
// nothing is mutated until build and every check have succeeded — the
// in-memory equivalent of one transaction.
func (s *Store) CommitBatch(_ context.Context, sc domain.Scope, id domain.HostID, seq uint64,
	build ingest.BuildFunc) (ingest.CommitResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, err := s.hostIn(sc, id)
	if err != nil {
		return ingest.CommitResult{}, err
	}
	if seq > domain.MaxCounter {
		return ingest.CommitResult{}, fmt.Errorf("seq %d exceeds storage range", seq)
	}
	if _, seen := h.seqs[seq]; seen {
		return ingest.CommitResult{Replayed: true}, nil
	}
	day := utcDay(s.now())
	p, err := build(ingest.TxState{Counters: cloneState(h.counters), LogBudget: s.logBudget(h.h.Tenant, day)})
	if err != nil {
		return ingest.CommitResult{}, fmt.Errorf("build batch: %w", err)
	}
	if p.SeqRetention <= 0 {
		return ingest.CommitResult{}, fmt.Errorf("prepared batch has no seq retention")
	}
	if err := fitsStorage(p); err != nil {
		return ingest.CommitResult{}, err
	}
	if err := fitsDetails(p); err != nil {
		return ingest.CommitResult{}, err
	}
	// Commit point: every fallible step is behind us.
	var res ingest.CommitResult
	for _, smp := range p.Samples {
		k := smp.TS.UnixMilli()
		if _, dup := h.samples[k]; dup {
			res.Conflicts++
			continue
		}
		h.samples[k] = smp
		h.rollup1m[k], h.rollup1h[k] = smp, smp
		res.Stored++
	}
	s.applyDetails(h, p.Details, &res)
	s.applyLogs(h, day, p, &res)
	s.applySnapshots(h, p.Processes, &res)
	if n := len(p.Samples); n > 0 {
		last := p.Samples[n-1]
		if h.latest == nil || last.TS.After(h.latest.TS) {
			h.latest = &last
		}
	}
	h.counters = cloneState(p.Counters)
	h.h.LastSeen = p.ReceivedAt
	h.h.Interval = p.Interval
	if p.Agent.Version != "" {
		h.h.AgentVersion = p.Agent.Version
	}
	h.h.Health.BufferDepth = p.Agent.BufferDepth
	h.h.Health.DroppedTotal = p.Agent.DroppedTotal
	h.h.Health.ClockSkew = p.Agent.ClockSkew
	h.h.Health.RejectedTotal += uint64(max(p.Rejected, 0))
	h.h.Health.ConflictTotal += uint64(max(res.Conflicts, 0))
	now := p.ReceivedAt.UnixMilli()
	h.seqs[seq] = now
	for q, at := range h.seqs {
		if at < now-p.SeqRetention.Milliseconds() {
			delete(h.seqs, q)
		}
	}
	return res, nil
}

// fitsStorage mirrors the SQLite adapter's signed 64-bit limit, so both
// adapters fail the same batches.
func fitsStorage(p ingest.Prepared) error {
	for _, smp := range p.Samples {
		for _, v := range []uint64{smp.MemoryUsed, smp.UptimeSeconds, smp.NetRxBytes, smp.NetTxBytes, smp.DiskReadBytes, smp.DiskWriteBytes} {
			if v > domain.MaxCounter {
				return fmt.Errorf("value %d does not fit a signed 64-bit column", v)
			}
		}
	}
	return nil
}

// roundGauges returns gauges at the two-decimal precision the ports promise
// (SQLite stores them as integer hundredths).
func roundGauges(s domain.CoreSample) domain.CoreSample {
	r := func(v float64) float64 { return math.Round(v*100) / 100 }
	s.CPUPercent, s.MemoryPercent, s.DiskPercent = r(s.CPUPercent), r(s.MemoryPercent), r(s.DiskPercent)
	s.Load1, s.Load5, s.Load15 = r(s.Load1), r(s.Load5), r(s.Load15)
	return s
}

func cloneState(st ingest.CounterState) ingest.CounterState {
	out := make(ingest.CounterState, len(st))
	for k, v := range st {
		out[k] = v
	}
	return out
}

// GetHost implements query.HostReader.
func (s *Store) GetHost(_ context.Context, sc domain.Scope, id domain.HostID) (domain.Host, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, err := s.hostIn(sc, id)
	if err != nil {
		return domain.Host{}, err
	}
	return h.h, nil
}

// ListHosts implements query.HostReader, ordered by name then id.
func (s *Store) ListHosts(_ context.Context, sc domain.Scope) ([]domain.Host, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return nil, err
	}
	var out []domain.Host
	for _, h := range s.hosts {
		if h.h.Tenant == tid {
			out = append(out, h.h)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID.String() < out[j].ID.String()
	})
	return out, nil
}

// RawCore implements query.CoreReader.
func (s *Store) RawCore(_ context.Context, sc domain.Scope, id domain.HostID, from, to time.Time) ([]domain.CoreSample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, err := s.hostIn(sc, id)
	if err != nil {
		return nil, err
	}
	var out []domain.CoreSample
	for _, smp := range h.samples {
		if !smp.TS.Before(from) && smp.TS.Before(to) {
			out = append(out, roundGauges(smp))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TS.Before(out[j].TS) })
	return out, nil
}
