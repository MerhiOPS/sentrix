package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// DB is the pair of pools every adapter uses: one writer connection, so all
// writes are serialised and BEGIN IMMEDIATE never contends, and a read pool
// that cannot write.
type DB struct {
	w    *sql.DB
	r    *sql.DB
	path string
}

// Open opens (creating if needed) the database at path and verifies that the
// connection settings the design depends on actually took effect. The writer
// opens first so a new file gets auto_vacuum before any table exists.
func Open(ctx context.Context, path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	w, err := openPool(ctx, path, roleWriter, 1)
	if err != nil {
		return nil, err
	}
	r, err := openPool(ctx, path, roleReader, max(4, runtime.NumCPU()))
	if err != nil {
		return nil, errors.Join(err, w.Close())
	}
	db := &DB{w: w, r: r, path: path}
	if err := db.verify(ctx); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return db, nil
}

// driverName is the database/sql driver used to open pools. Tests swap in a
// recording wrapper to audit every statement the adapter issues.
var driverName = "sqlite"

func openPool(ctx context.Context, path string, role connRole, conns int) (*sql.DB, error) {
	p, err := sql.Open(driverName, dsn(path, role))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	p.SetMaxOpenConns(conns)
	p.SetMaxIdleConns(conns)
	if err := p.PingContext(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("ping sqlite: %w", err), p.Close())
	}
	return p, nil
}

// verify refuses to run on a connection where a load-bearing pragma did not
// apply. Each of these fails silently otherwise: FKs that do not enforce, a
// reader that can write, a rollback journal that blocks readers.
func (db *DB) verify(ctx context.Context) error {
	checks := []struct {
		pool  *sql.DB
		name  string
		query string
		want  string
	}{
		{db.w, "writer foreign_keys", "PRAGMA foreign_keys", "1"},
		{db.w, "writer journal_mode", "PRAGMA journal_mode", "wal"},
		{db.w, "writer secure_delete", "PRAGMA secure_delete", "1"},
		{db.r, "reader foreign_keys", "PRAGMA foreign_keys", "1"},
		{db.r, "reader query_only", "PRAGMA query_only", "1"},
	}
	for _, c := range checks {
		var got string
		if err := c.pool.QueryRowContext(ctx, c.query).Scan(&got); err != nil {
			return fmt.Errorf("verify %s: %w", c.name, err)
		}
		if got != c.want {
			return fmt.Errorf("verify %s: got %q, want %q", c.name, got, c.want)
		}
	}
	return nil
}

// Writer returns the single-connection write pool.
func (db *DB) Writer() *sql.DB { return db.w }

// Path is the database file (its WAL is Path + "-wal").
func (db *DB) Path() string { return db.path }

// Reader returns the read-only pool.
func (db *DB) Reader() *sql.DB { return db.r }

// Close closes both pools.
func (db *DB) Close() error {
	rerr := db.r.Close()
	werr := db.w.Close()
	if werr != nil {
		return werr
	}
	return rerr
}
