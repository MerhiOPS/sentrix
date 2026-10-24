package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func openTemp(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "with space", "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeDB(t, db) })
	return db
}

// closeDB checks Close: it closes the writer, so its error is not noise.
func closeDB(t *testing.T, db *DB) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Errorf("close db: %v", err)
	}
}

func tableExists(t *testing.T, db *DB, name string) bool {
	t.Helper()
	var n int
	err := db.Reader().QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func TestMigrateAppliesInOrderAndIsIdempotent(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	fsys := fstest.MapFS{
		// 0002 depends on 0001, so lexical order is load-bearing.
		"0002_b.sql": {Data: []byte(`ALTER TABLE a ADD COLUMN y INTEGER;`)},
		"0001_a.sql": {Data: []byte("CREATE TABLE a (x INTEGER);\nCREATE TABLE a2 (z INTEGER);")},
		"README.md":  {Data: []byte("not a migration")},
	}
	if err := Migrate(ctx, db, fsys); err != nil {
		t.Fatal(err)
	}
	if !tableExists(t, db, "a") || !tableExists(t, db, "a2") {
		t.Fatal("multi-statement migration did not fully apply")
	}
	if err := Migrate(ctx, db, fsys); err != nil {
		t.Fatalf("second run must be a no-op, got %v", err)
	}
	var n int
	if err := db.Reader().QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("schema_migrations rows = %d, %v; want 2", n, err)
	}
}

func TestMigrateRefusesEditedMigration(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	fsys := fstest.MapFS{"0001_a.sql": {Data: []byte(`CREATE TABLE a (x INTEGER);`)}}
	if err := Migrate(ctx, db, fsys); err != nil {
		t.Fatal(err)
	}
	fsys["0001_a.sql"] = &fstest.MapFile{Data: []byte(`CREATE TABLE a (x INTEGER, y TEXT);`)}
	if err := Migrate(ctx, db, fsys); !errors.Is(err, ErrMigrationChanged) {
		t.Fatalf("err = %v, want ErrMigrationChanged", err)
	}
}

func TestMigrateLineEndingsAreNotAnEdit(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	if err := Migrate(ctx, db, fstest.MapFS{
		"0001_a.sql": {Data: []byte("CREATE TABLE a (x INTEGER);\n")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db, fstest.MapFS{
		"0001_a.sql": {Data: []byte("CREATE TABLE a (x INTEGER);\r\n")},
	}); err != nil {
		t.Fatalf("CRLF checkout treated as an edit: %v", err)
	}
}

func TestMigrateFailureLeavesNoPartialState(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	fsys := fstest.MapFS{
		"0001_bad.sql": {Data: []byte("CREATE TABLE half (x INTEGER);\nTHIS IS NOT SQL;")},
	}
	if err := Migrate(ctx, db, fsys); err == nil {
		t.Fatal("broken migration applied without error")
	}
	if tableExists(t, db, "half") {
		t.Fatal("first statement of a failed migration was left behind")
	}
	var n int
	if err := db.Reader().QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("failed migration was recorded: n=%d err=%v", n, err)
	}
}
