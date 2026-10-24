// Package sqlite is the SQLite storage adapter. It is the only package in the
// repository allowed to contain SQL (CLAUDE.md, enforced by scripts/ci).
package sqlite

import (
	"net/url"
	"strconv"
)

// busyTimeoutMS bounds how long a connection waits on a lock before
// returning SQLITE_BUSY. Writes are serialised through one connection, so
// this mostly covers readers waiting on a checkpoint.
const busyTimeoutMS = 5000

// connRole selects the pragmas for a connection pool.
type connRole int

const (
	roleWriter connRole = iota
	roleReader
)

// dsn builds a modernc.org/sqlite DSN. Pragmas are per connection and
// silently revert to SQLite's defaults on any connection that did not set
// them, so they go in the DSN — applied to every connection the pool opens —
// never in a one-off Exec (PLAN §4).
func dsn(path string, role connRole) string {
	q := url.Values{}
	pragmas := []string{
		"busy_timeout(" + strconv.Itoa(busyTimeoutMS) + ")",
		"foreign_keys(1)",   // off by default: without it every FK is decoration
		"journal_mode(WAL)", // readers never block the writer
		"synchronous(NORMAL)",
	}
	switch role {
	case roleWriter:
		pragmas = append(pragmas,
			// Only takes effect before the first table exists, which is
			// exactly when the writer first opens a new file.
			"auto_vacuum(INCREMENTAL)",
			// Deleted content is overwritten, not left in free pages (§4.3).
			"secure_delete(1)",
		)
		// BEGIN IMMEDIATE: take the write lock at BEGIN, so a transaction
		// that reads then writes cannot fail with SQLITE_BUSY halfway.
		q.Set("_txlock", "immediate")
	case roleReader:
		pragmas = append(pragmas, "query_only(1)") // a write here is a bug: fail it
	}
	for _, p := range pragmas {
		q.Add("_pragma", p)
	}
	// A plain path, not a file: URI — a URI would percent-decode the path, and
	// the driver splits the name from the options at the first '?'.
	return path + "?" + q.Encode()
}
