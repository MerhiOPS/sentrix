package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
)

// ErrMigrationChanged means a migration that was already applied has been
// edited. Applied migrations are immutable: the fix is a new migration.
var ErrMigrationChanged = errors.New("applied migration was modified")

const createMigrationsTable = `CREATE TABLE IF NOT EXISTS schema_migrations (
	name       TEXT PRIMARY KEY,
	checksum   TEXT NOT NULL,
	applied_at INTEGER NOT NULL
) STRICT`

// Migrate applies every *.sql file in fsys that is not yet recorded, in
// lexical order, each in its own transaction together with its record. It
// first checks the checksum of every applied file and refuses to run if one
// changed (OLD: a migrator that replayed every file was a production defect).
func Migrate(ctx context.Context, db *DB, fsys fs.FS) error {
	w := db.Writer()
	if _, err := w.ExecContext(ctx, createMigrationsTable); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	files, err := migrationFiles(fsys)
	if err != nil {
		return err
	}
	applied, err := appliedChecksums(ctx, w)
	if err != nil {
		return err
	}
	for _, name := range files {
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		sum := checksum(body)
		if prev, ok := applied[name]; ok {
			if prev != sum {
				return fmt.Errorf("%w: %s", ErrMigrationChanged, name)
			}
			continue
		}
		if err := applyMigration(ctx, w, name, string(body), sum); err != nil {
			return err
		}
	}
	return nil
}

func migrationFiles(fsys fs.FS) ([]string, error) {
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(names)
	return names, nil
}

func appliedChecksums(ctx context.Context, w *sql.DB) (map[string]string, error) {
	rows, err := w.QueryContext(ctx, `SELECT name, checksum FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, sum string
		if err := rows.Scan(&name, &sum); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		out[name] = sum
	}
	return out, rows.Err()
}

func applyMigration(ctx context.Context, w *sql.DB, name, body, sum string) error {
	tx, err := w.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	if _, err := tx.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (name, checksum, applied_at) VALUES (?, ?, ?)`,
		name, sum, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}
	return nil
}

// checksum normalises line endings first, so a Windows checkout with CRLF
// does not look like an edited migration.
func checksum(body []byte) string {
	norm := strings.ReplaceAll(string(body), "\r\n", "\n")
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])
}
