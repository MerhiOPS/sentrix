package sqlite

import (
	"errors"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Store implements the application ports on SQLite. Writes go through the
// single writer connection; reads through the query_only pool.
type Store struct {
	db     *DB
	series seriesCache
	stmts  *stmtCache
	now    func() time.Time // the log quota's accounting clock
}

// NewStore wraps an opened, migrated database.
func NewStore(db *DB) *Store { return &Store{db: db, stmts: newStmtCache(db.Writer()), now: time.Now} }

// SetClock replaces the accounting clock (tests).
func (s *Store) SetClock(now func() time.Time) { s.now = now }

// isUniqueViolation reports a UNIQUE or PRIMARY KEY constraint failure.
func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		c := se.Code()
		return c == sqlite3.SQLITE_CONSTRAINT_UNIQUE || c == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
