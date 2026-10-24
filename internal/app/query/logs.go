package query

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// LogCursor is the keyset position of the last entry on a page.
type LogCursor struct {
	TS time.Time
	ID int64
}

// LogQuery is a validated search. Results are newest first: (ts, id) DESC,
// strictly before Before when set.
type LogQuery struct {
	Hosts   []domain.HostID // empty: every host of the tenant
	Levels  []domain.Level  // empty: every level
	Clauses []Clause
	From    time.Time
	To      time.Time
	Before  *LogCursor
	Limit   int
}

// LogEntry is one stored line.
type LogEntry struct {
	ID   int64
	Host domain.HostID
	Line domain.LogLine
}

// LogReader searches stored logs within a scope.
type LogReader interface {
	SearchLogs(ctx context.Context, s domain.Scope, q LogQuery) ([]LogEntry, error)
}

// Page limits.
const (
	DefaultLogLimit = 100
	MaxLogLimit     = 500
)

// LogSearchRequest is the raw request from the API.
type LogSearchRequest struct {
	Hosts  []domain.HostID
	Levels []domain.Level
	Q      string
	From   time.Time
	To     time.Time
	Cursor string
	Limit  int
}

// LogPage is one page and the cursor for the next ("" at the end).
type LogPage struct {
	Entries []LogEntry
	Next    string
}

// Logs answers log searches.
type Logs struct{ reader LogReader }

// NewLogs builds the use case.
func NewLogs(r LogReader) *Logs { return &Logs{reader: r} }

// Search validates the request and fetches one page.
func (l *Logs) Search(ctx context.Context, s domain.Scope, req LogSearchRequest) (LogPage, error) {
	clauses, err := ParseSearch(req.Q)
	if err != nil {
		return LogPage{}, err
	}
	if !req.To.After(req.From) {
		return LogPage{}, fmt.Errorf("%w: to must be after from", domain.ErrInvalid)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = DefaultLogLimit
	}
	q := LogQuery{Hosts: req.Hosts, Levels: req.Levels, Clauses: clauses, From: req.From, To: req.To, Limit: min(limit, MaxLogLimit)}
	if req.Cursor != "" {
		c, err := DecodeCursor(req.Cursor)
		if err != nil {
			return LogPage{}, err
		}
		q.Before = &c
	}
	entries, err := l.reader.SearchLogs(ctx, s, q)
	if err != nil {
		return LogPage{}, err
	}
	page := LogPage{Entries: entries}
	if len(entries) == q.Limit {
		last := entries[len(entries)-1]
		page.Next = EncodeCursor(LogCursor{TS: last.Line.TS, ID: last.ID})
	}
	return page, nil
}

// EncodeCursor renders an opaque cursor.
func EncodeCursor(c LogCursor) string {
	raw := strconv.FormatInt(c.TS.UnixMilli(), 10) + "." + strconv.FormatInt(c.ID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor parses and validates a cursor (OLD B9: a malformed cursor is
// a 400, never a 500 or a silently different query).
func DecodeCursor(s string) (LogCursor, error) {
	bad := fmt.Errorf("%w: malformed cursor", domain.ErrInvalid)
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return LogCursor{}, bad
	}
	tsPart, idPart, ok := strings.Cut(string(raw), ".")
	if !ok {
		return LogCursor{}, bad
	}
	ms, err1 := strconv.ParseInt(tsPart, 10, 64)
	id, err2 := strconv.ParseInt(idPart, 10, 64)
	if err1 != nil || err2 != nil || ms < 0 || id <= 0 {
		return LogCursor{}, bad
	}
	return LogCursor{TS: time.UnixMilli(ms).UTC(), ID: id}, nil
}
