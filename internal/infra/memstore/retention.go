package memstore

import (
	"context"
	"slices"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/retention"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// RetentionTargets implements retention.Store.
func (s *Store) RetentionTargets(_ context.Context, ops domain.OpsScope) ([]retention.Target, error) {
	if _, err := ops.Reason(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []retention.Target
	for _, h := range s.hosts {
		out = append(out, retention.Target{Tenant: h.h.Tenant, Host: h.h.ID})
	}
	return out, nil
}

// PruneHost implements retention.Store. A rollup row is pruned by bucket
// start, like SQLite's, so the whole bucket goes at once.
func (s *Store) PruneHost(_ context.Context, ops domain.OpsScope, t retention.Target, k domain.DataKind,
	before time.Time, limit int) (int, error) {
	h, err := s.target(ops, t)
	if err != nil {
		return 0, err
	}
	defer s.mu.Unlock()
	cut := before.UnixMilli()
	switch k {
	case domain.DataRawCore:
		return pruneMap(h.samples, func(ts int64) bool { return ts < cut }, limit), nil
	case domain.DataProcesses:
		return pruneMap(h.snaps, func(ts int64) bool { return ts < cut }, limit), nil
	case domain.DataRollup1m:
		return pruneMap(h.rollup1m, func(ts int64) bool { return bucketMS(ts, time.Minute) < cut }, limit), nil
	case domain.DataRollup1h:
		return pruneMap(h.rollup1h, func(ts int64) bool { return bucketMS(ts, time.Hour) < cut }, limit), nil
	case domain.DataDetail:
		n := 0
		for _, ms := range h.series {
			n += pruneMap(ms.points, func(ts int64) bool { return ts < cut }, limit-n)
		}
		return n, nil
	case domain.DataLogs:
		n := 0
		h.logs = slices.DeleteFunc(h.logs, func(r logRow) bool {
			if n < limit && r.line.TS.UnixMilli() < cut {
				n++
				return true
			}
			return false
		})
		return n, nil
	default:
		return 0, domain.ErrInvalid
	}
}

// OldestHost implements retention.Store.
func (s *Store) OldestHost(_ context.Context, ops domain.OpsScope, t retention.Target, k domain.DataKind) (time.Time, bool, error) {
	h, err := s.target(ops, t)
	if err != nil {
		return time.Time{}, false, err
	}
	defer s.mu.Unlock()
	var keys []int64
	switch k {
	case domain.DataRawCore:
		keys = mapKeys(h.samples)
	case domain.DataProcesses:
		keys = mapKeys(h.snaps)
	case domain.DataRollup1m:
		for _, ts := range mapKeys(h.rollup1m) {
			keys = append(keys, bucketMS(ts, time.Minute))
		}
	case domain.DataRollup1h:
		for _, ts := range mapKeys(h.rollup1h) {
			keys = append(keys, bucketMS(ts, time.Hour))
		}
	case domain.DataDetail:
		for _, ms := range h.series {
			keys = append(keys, mapKeys(ms.points)...)
		}
	case domain.DataLogs:
		for _, r := range h.logs {
			keys = append(keys, r.line.TS.UnixMilli())
		}
	}
	if len(keys) == 0 {
		return time.Time{}, false, nil
	}
	return time.UnixMilli(slices.Min(keys)).UTC(), true, nil
}

// Compact implements retention.Store; there is nothing to compact in memory.
func (s *Store) Compact(_ context.Context, ops domain.OpsScope) error {
	_, err := ops.Reason()
	return err
}

// target resolves a retention target and returns with mu held.
func (s *Store) target(ops domain.OpsScope, t retention.Target) (*host, error) {
	if _, err := ops.Reason(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	h, ok := s.hosts[t.Host]
	if !ok || h.h.Tenant != t.Tenant {
		s.mu.Unlock()
		return nil, domain.ErrNotFound
	}
	return h, nil
}

// pruneMap deletes up to limit keys matching old, oldest first.
func pruneMap[V any](m map[int64]V, old func(int64) bool, limit int) int {
	keys := mapKeys(m)
	slices.Sort(keys)
	n := 0
	for _, k := range keys {
		if n >= limit || !old(k) {
			break
		}
		delete(m, k)
		n++
	}
	return n
}

func mapKeys[V any](m map[int64]V) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func bucketMS(ts int64, w time.Duration) int64 { return ts - ts%w.Milliseconds() }
