package sqlite

import (
	"errors"
	"strings"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Store implements the application ports on SQLite. Writes go through the
// single writer connection; reads through the query_only pool.
type Store struct {
	db     *DB
	series seriesCache
}

// NewStore wraps an opened, migrated database.
func NewStore(db *DB) *Store { return &Store{db: db} }

// isUniqueViolation reports a UNIQUE or PRIMARY KEY constraint failure.
func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		c := se.Code()
		return c == sqlite3.SQLITE_CONSTRAINT_UNIQUE || c == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
