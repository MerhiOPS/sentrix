package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestAssumeVacuumIntoIsConsistentUnderConcurrentWriter backs the Phase 7
// backup design: VACUUM INTO runs in one read transaction, so a backup taken
// while a writer commits paired rows into two tables must contain both rows
// of every pair, never one. OLD's ClickHouse BACKUP failed exactly this.
func TestAssumeVacuumIntoIsConsistentUnderConcurrentWriter(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(context.Background(), filepath.Join(dir, "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB(t, db)
	w := db.Writer()
	mustExec(t, w, `CREATE TABLE a (marker INTEGER PRIMARY KEY)`)
	mustExec(t, w, `CREATE TABLE b (marker INTEGER PRIMARY KEY)`)

	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 1; !stop.Load(); i++ {
			if err := writePair(w, i); err != nil {
				t.Errorf("writer: %v", err)
				return
			}
		}
	}()

	backups := 0
	deadline := time.Now().Add(400 * time.Millisecond)
	for ; time.Now().Before(deadline); backups++ {
		out := filepath.Join(dir, fmt.Sprintf("backup-%d.db", backups))
		// The read pool is query_only; VACUUM INTO writes a new file, so it
		// runs on its own plain connection to the live database.
		if err := vacuumInto(filepath.Join(dir, "live.db"), out); err != nil {
			t.Fatal(err)
		}
		assertPairsMatch(t, out)
	}
	stop.Store(true)
	wg.Wait()
	if backups < 2 {
		t.Fatalf("only %d backups taken; the test did not exercise concurrency", backups)
	}
}

func writePair(w *sql.DB, i int) error {
	tx, err := w.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`INSERT INTO a VALUES (?)`, i); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO b VALUES (?)`, i); err != nil {
		return err
	}
	return tx.Commit()
}

func vacuumInto(live, out string) error {
	c, err := sql.Open("sqlite", live+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer c.Close()
	_, err = c.Exec(`VACUUM INTO ?`, out)
	return err
}

func assertPairsMatch(t *testing.T, path string) {
	t.Helper()
	c, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var na, nb, maxA, maxB int
	q := `SELECT (SELECT count(*) FROM a), (SELECT count(*) FROM b),
	             (SELECT coalesce(max(marker), 0) FROM a), (SELECT coalesce(max(marker), 0) FROM b)`
	if err := c.QueryRow(q).Scan(&na, &nb, &maxA, &maxB); err != nil {
		t.Fatal(err)
	}
	if na != nb || maxA != maxB {
		t.Fatalf("backup %s is torn: a=%d (max %d) b=%d (max %d)", filepath.Base(path), na, maxA, nb, maxB)
	}
}
