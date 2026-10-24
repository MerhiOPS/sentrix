package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// ftsQuery renders clauses as an FTS5 query in which every term is a
// double-quoted string (embedded quotes doubled). Nothing a user types can
// become an FTS5 operator, column filter or syntax error.
func ftsQuery(clauses []query.Clause) string {
	parts := make([]string, 0, len(clauses))
	for _, c := range clauses {
		terms := make([]string, len(c))
		for i, t := range c {
			terms[i] = `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
		}
		if len(terms) == 1 {
			parts = append(parts, terms[0])
		} else {
			parts = append(parts, "("+strings.Join(terms, " OR ")+")")
		}
	}
	return strings.Join(parts, " AND ")
}

// SearchLogs implements query.LogReader.
func (s *Store) SearchLogs(ctx context.Context, sc domain.Scope, q query.LogQuery) ([]query.LogEntry, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	where := []string{"l.tenant_key = ?", "l.ts >= ?", "l.ts < ?"}
	args := []any{tk, encTime(q.From), encTime(q.To)}
	if len(q.Hosts) > 0 {
		keys, err := s.hostKeys(ctx, tk, q.Hosts)
		if err != nil {
			return nil, err
		}
		if len(keys) == 0 {
			return nil, nil // none of the hosts is this tenant's
		}
		where = append(where, "l.host_key IN ("+placeholders(len(keys))+")")
		args = append(args, keys...)
	}
	if len(q.Levels) > 0 {
		where = append(where, "l.level IN ("+placeholders(len(q.Levels))+")")
		for _, lv := range q.Levels {
			args = append(args, string(lv))
		}
	}
	if q.Before != nil {
		where = append(where, "(l.ts < ? OR (l.ts = ? AND l.id < ?))")
		bt := encTime(q.Before.TS)
		args = append(args, bt, bt, q.Before.ID)
	}
	if len(q.Clauses) > 0 {
		where = append(where, "l.id IN (SELECT rowid FROM logs_fts WHERE logs_fts MATCH ?)")
		args = append(args, ftsQuery(q.Clauses))
	}
	args = append(args, q.Limit)
	//nolint:gosec // only fixed fragments and placeholders are concatenated; every value is bound
	rows, err := r.QueryContext(ctx, `SELECT l.id, h.id, l.ts, l.source, l.level, l.message, l.labels
		FROM logs l JOIN hosts h ON h.key = l.host_key AND h.tenant_key = l.tenant_key
		WHERE `+strings.Join(where, " AND ")+` ORDER BY l.ts DESC, l.id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("search logs: %w", err)
	}
	defer rows.Close()
	var out []query.LogEntry
	for rows.Next() {
		e, err := scanLog(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanLog(scan func(...any) error) (query.LogEntry, error) {
	var e query.LogEntry
	var hostRaw []byte
	var ts int64
	var level, labels string
	if err := scan(&e.ID, &hostRaw, &ts, &e.Line.Source, &level, &e.Line.Message, &labels); err != nil {
		return e, fmt.Errorf("scan log: %w", err)
	}
	hid, err := decID(hostRaw)
	if err != nil {
		return e, err
	}
	e.Host, e.Line.TS, e.Line.Level = domain.HostID(hid), decTime(ts), domain.Level(level)
	if err := json.Unmarshal([]byte(labels), &e.Line.Labels); err != nil {
		return e, fmt.Errorf("decode log labels: %w", err)
	}
	return e, nil
}

// hostKeys resolves host ids within the tenant; unknown ids are skipped.
func (s *Store) hostKeys(ctx context.Context, tk int64, hosts []domain.HostID) ([]any, error) {
	var keys []any
	for _, h := range hosts {
		k, err := hostKey(ctx, s.db.Reader(), tk, h)
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?, ", n), ", ") }
