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
	var written atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 1; !stop.Load(); i++ {
			if err := writePair(w, i); err != nil {
				t.Errorf("writer: %v", err)
				return
			}
			written.Store(int64(i))
		}
	}()

	// Count backups, not wall time: a slow runner (Windows CI) may fit only
	// one backup into a fixed window. Run until at least minBackups were
	// taken with the writer advancing between them, capped for safety.
	const minBackups = 3
	backups, overlapped := 0, 0
	start := time.Now()
	for (backups < minBackups || time.Since(start) < 400*time.Millisecond) && time.Since(start) < 15*time.Second {
		before := written.Load()
		out := filepath.Join(dir, fmt.Sprintf("backup-%d.db", backups))
		// The read pool is query_only; VACUUM INTO writes a new file, so it
		// runs on its own plain connection to the live database.
		if err := vacuumInto(filepath.Join(dir, "live.db"), out); err != nil {
			t.Fatal(err)
		}
		assertPairsMatch(t, out)
		backups++
		if written.Load() > before {
			overlapped++ // the writer committed while this backup ran
		}
	}
	stop.Store(true)
	wg.Wait()
	if backups < minBackups || overlapped < 2 {
		t.Fatalf("%d backups, %d overlapping a write: the test did not exercise concurrency", backups, overlapped)
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
