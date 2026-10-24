package sqlite

// Assumption tests (PLAN Phase 0): every SQLite behaviour the plan relies
// on, asserted against the pinned modernc.org/sqlite. If one of these fails
// after a driver bump, the plan changes before the code does.

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rawOpen(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func mustExec(t *testing.T, db interface {
	Exec(string, ...any) (sql.Result, error)
}, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func TestAssumeFTS5TrigramSubstringCaseInsensitive(t *testing.T) {
	db := openTemp(t)
	w := db.Writer()
	mustExec(t, w, `CREATE VIRTUAL TABLE f USING fts5(message, tokenize = 'trigram case_sensitive 0')`)
	mustExec(t, w, `INSERT INTO f (message) VALUES ('Connection REFUSED by upstream'), ('all good')`)
	var n int
	if err := db.Reader().QueryRow(`SELECT count(*) FROM f WHERE f MATCH ?`, `"efuse"`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("substring 'efuse' matched %d rows, want 1", n)
	}
}

func TestAssumeForeignKeysOffByDefaultPerConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fk.db")
	plain := rawOpen(t, path)
	var on int
	if err := plain.QueryRow(`PRAGMA foreign_keys`).Scan(&on); err != nil || on != 0 {
		t.Fatalf("default foreign_keys = %d (%v); the plan assumes 0", on, err)
	}
	mustExec(t, plain, `CREATE TABLE p (id INTEGER PRIMARY KEY)`)
	mustExec(t, plain, `CREATE TABLE c (pid INTEGER REFERENCES p(id))`)
	mustExec(t, plain, `INSERT INTO c VALUES (42)`) // accepted: FKs not enforced
	plain.Close()

	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB(t, db)
	if _, err := db.Writer().Exec(`INSERT INTO c VALUES (43)`); err == nil {
		t.Fatal("FK violation accepted on a connection opened by Open")
	}
}

func TestAssumeQueryOnlyRejectsWrites(t *testing.T) {
	db := openTemp(t)
	mustExec(t, db.Writer(), `CREATE TABLE t (x INTEGER)`)
	if _, err := db.Reader().Exec(`INSERT INTO t VALUES (1)`); err == nil {
		t.Fatal("reader pool accepted a write")
	}
}

func TestAssumeTxLockImmediateTakesWriteLockAtBegin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock.db")
	setup := rawOpen(t, path+"?_pragma=journal_mode(WAL)")
	mustExec(t, setup, `CREATE TABLE t (x INTEGER)`)

	for _, c := range []struct {
		lock      string
		wantBlock bool
	}{{"immediate", true}, {"deferred", false}} {
		holder := rawOpen(t, path+"?_txlock="+c.lock)
		other := rawOpen(t, path+"?_pragma=busy_timeout(50)")
		tx, err := holder.Begin()
		if err != nil {
			t.Fatal(err)
		}
		_, err = other.Exec(`INSERT INTO t VALUES (1)`)
		blocked := err != nil
		tx.Rollback()
		if blocked != c.wantBlock {
			t.Fatalf("_txlock=%s: concurrent write blocked=%v (err %v), want %v",
				c.lock, blocked, err, c.wantBlock)
		}
	}
}

func TestAssumeAutoVacuumIncrementalOnNewFile(t *testing.T) {
	db := openTemp(t)
	var mode int
	if err := db.Writer().QueryRow(`PRAGMA auto_vacuum`).Scan(&mode); err != nil || mode != 2 {
		t.Fatalf("auto_vacuum = %d (%v), want 2 (incremental)", mode, err)
	}
}

// canarySurvives inserts a canary, deletes it, checkpoints, and reports
// whether its bytes are still anywhere in the database or WAL file.
func canarySurvives(t *testing.T, secureDelete string) bool {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sd.db")
	db := rawOpen(t, path+"?_pragma=journal_mode(WAL)&_pragma=secure_delete("+secureDelete+")")
	canary := "CANARY-" + strings.Repeat("9f3a", 8)
	mustExec(t, db, `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`)
	mustExec(t, db, `INSERT INTO t (v) VALUES ('keep me'), (?)`, canary)
	mustExec(t, db, `PRAGMA wal_checkpoint(TRUNCATE)`)
	mustExec(t, db, `DELETE FROM t WHERE v = ?`, canary)
	mustExec(t, db, `PRAGMA wal_checkpoint(TRUNCATE)`)
	for _, f := range []string{path, path + "-wal"} {
		data, err := os.ReadFile(f)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(canary)) {
			return true
		}
	}
	return false
}

func TestAssumeSecureDeleteZeroesContent(t *testing.T) {
	if !canarySurvives(t, "0") {
		t.Fatal("control failed: without secure_delete the canary should survive, so the scan proves nothing")
	}
	if canarySurvives(t, "1") {
		t.Fatal("secure_delete=1 left the deleted canary on disk")
	}
}
