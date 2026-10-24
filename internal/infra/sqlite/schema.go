package sqlite

import (
	"context"
	"embed"
	"errors"
	"io/fs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

var migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // the embed pattern above guarantees the directory exists
	}
	migrations = sub
}

// Migrations returns the embedded schema migrations.
func Migrations() fs.FS { return migrations }

// OpenMigrated opens the database and applies every pending migration.
func OpenMigrated(ctx context.Context, path string) (*DB, error) {
	db, err := Open(ctx, path)
	if err != nil {
		return nil, err
	}
	if err := Migrate(ctx, db, migrations); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return db, nil
}
