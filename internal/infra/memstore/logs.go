package memstore

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// defaultLogBytesPerDay mirrors the SQLite column default.
const defaultLogBytesPerDay = 1 << 30

type logRow struct {
	id   int64
	line domain.LogLine
}

type usageKey struct {
	tenant domain.TenantID
	day    int64
}

func utcDay(t time.Time) int64 { return t.UTC().Unix() / 86400 }

// logBudget is the tenant's bytes left on day. Caller holds mu.
func (s *Store) logBudget(t domain.TenantID, day int64) int64 {
	limit := int64(defaultLogBytesPerDay)
	if l, ok := s.logLimits[t]; ok {
		limit = l
	}
	return limit - s.logUsage[usageKey{t, day}]
}

// applyLogs stores lines and meters the batch. Caller holds mu; every
// fallible check has passed.
func (s *Store) applyLogs(h *host, day int64, p ingest.Prepared, res *ingest.CommitResult) {
	for _, l := range p.Logs {
		s.nextLog++
		cp := l
		cp.Labels = domain.Labels{}
		for k, v := range l.Labels {
			cp.Labels[k] = v
		}
		h.logs = append(h.logs, logRow{id: s.nextLog, line: cp})
	}
	res.LogsStored = len(p.Logs)
	s.logUsage[usageKey{h.h.Tenant, day}] += p.LogStats.Bytes
}

// SearchLogs implements query.LogReader with the reference semantics
// (query.Matches), which the contract pins the SQLite FTS path to.
func (s *Store) SearchLogs(_ context.Context, sc domain.Scope, q query.LogQuery) ([]query.LogEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return nil, err
	}
	hostOK := map[domain.HostID]bool{}
	for _, id := range q.Hosts {
		hostOK[id] = true
	}
	levelOK := map[domain.Level]bool{}
	for _, l := range q.Levels {
		levelOK[l] = true
	}
	var out []query.LogEntry
	for id, h := range s.hosts {
		if h.h.Tenant != tid || (len(hostOK) > 0 && !hostOK[id]) {
			continue
		}
		for _, r := range h.logs {
			if matchesQuery(r, q, levelOK) {
				out = append(out, query.LogEntry{ID: r.id, Host: id, Line: r.line})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Line.TS.Equal(out[j].Line.TS) {
			return out[i].Line.TS.After(out[j].Line.TS)
		}
		return out[i].ID > out[j].ID
	})
	if len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func matchesQuery(r logRow, q query.LogQuery, levelOK map[domain.Level]bool) bool {
	ts := r.line.TS
	if ts.Before(q.From) || !ts.Before(q.To) {
		return false
	}
	if len(levelOK) > 0 && !levelOK[r.line.Level] {
		return false
	}
	if b := q.Before; b != nil && !olderThan(ts, r.id, *b) {
		return false
	}
	return query.Matches(q.Clauses, r.line.Message)
}

// olderThan reports whether (ts, id) sorts after cursor c in newest-first
// order — the keyset condition the SQLite adapter expresses in SQL.
func olderThan(ts time.Time, id int64, c query.LogCursor) bool {
	return ts.Before(c.TS) || (ts.Equal(c.TS) && id < c.ID)
}

// SetLogLimit implements identity.TenantRepo.
func (s *Store) SetLogLimit(_ context.Context, ops domain.OpsScope, t domain.TenantID, bytes int64) error {
	if _, err := ops.Reason(); err != nil {
		return err
	}
	if bytes < 0 {
		return fmt.Errorf("%w: log limit must be >= 0", domain.ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tenants[t]; !ok {
		return fmt.Errorf("tenant: %w", domain.ErrNotFound)
	}
	s.logLimits[t] = bytes
	return nil
}

// RedactionRules implements redaction.RuleRepo.
func (s *Store) RedactionRules(_ context.Context, sc domain.Scope) ([]domain.RedactionRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return nil, err
	}
	return append([]domain.RedactionRule(nil), s.rules[tid]...), nil
}

// CreateRedactionRule implements redaction.RuleRepo.
func (s *Store) CreateRedactionRule(_ context.Context, sc domain.Scope, r domain.RedactionRule) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return 0, err
	}
	s.nextRule++
	r.ID = s.nextRule
	s.rules[tid] = append(s.rules[tid], r)
	return r.ID, nil
}

// DeleteRedactionRule implements redaction.RuleRepo.
func (s *Store) DeleteRedactionRule(_ context.Context, sc domain.Scope, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, err := s.tenantOf(sc)
	if err != nil {
		return err
	}
	for i, r := range s.rules[tid] {
		if r.ID == id {
			s.rules[tid] = append(s.rules[tid][:i], s.rules[tid][i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("redaction rule: %w", domain.ErrNotFound)
}
