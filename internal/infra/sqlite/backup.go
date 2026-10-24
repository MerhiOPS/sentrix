package sqlite

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Backup writes a consistent copy of the database to dest with VACUUM INTO:
// one read transaction, so every table is from the same instant (unlike a
// per-table dump), free pages are not copied, and ingest keeps writing
// while it runs. dest must not exist.
func (s *Store) Backup(ctx context.Context, ops domain.OpsScope, dest string) error {
	if _, err := ops.Reason(); err != nil {
		return err
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%w: %s already exists", domain.ErrConflict, dest)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return fmt.Errorf("backup dir: %w", err)
	}
	conn, err := openPool(ctx, s.db.Path(), roleBackup, 1)
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `VACUUM INTO ?`, dest); err != nil {
		return errors.Join(fmt.Errorf("vacuum into: %w", err), conn.Close())
	}
	if err := conn.Close(); err != nil {
		return err
	}
	return os.Chmod(dest, 0o600)
}

// Restore copies a backup into dataDir as its database, then opens it the
// way the server does — every pragma verified, pending migrations applied —
// and checks its integrity. dataDir must not already hold a database: a
// restore never overwrites live data.
func Restore(ctx context.Context, backup, dataDir, dbName string) error {
	dest := filepath.Join(dataDir, dbName)
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%w: %s already holds a database; restore into an empty directory", domain.ErrConflict, dataDir)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	if err := copyFile(backup, dest); err != nil {
		return err
	}
	fail := func(err error) error { return errors.Join(err, os.Remove(dest)) }
	db, err := OpenMigrated(ctx, dest)
	if err != nil {
		return fail(fmt.Errorf("open restored database: %w", err))
	}
	var ok string
	if err := db.Reader().QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&ok); err != nil || ok != "ok" {
		return fail(errors.Join(fmt.Errorf("integrity check: %q", ok), err, db.Close()))
	}
	return db.Close()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // an operator-given backup path
	if err != nil {
		return fmt.Errorf("open backup: %w", err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // inside the data dir
	if err != nil {
		return fmt.Errorf("create database: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		return errors.Join(fmt.Errorf("copy backup: %w", err), out.Close())
	}
	if err := out.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync: %w", err), out.Close())
	}
	return out.Close()
}

// TableDigest returns each table's row count and a checksum of its content
// in key order, for comparing a restore with its source.
func (s *Store) TableDigest(ctx context.Context) (map[string]string, error) {
	return digest(ctx, s.db.Reader())
}
