package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAppliesPragmasToEveryPooledConnection(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	// Hold several reader connections at once so the pool must open new
	// ones; each must carry the pragmas, not just the first.
	var conns []*sql.Conn
	for i := 0; i < 4; i++ {
		c, err := db.Reader().Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	for i, c := range conns {
		var fk, qo int
		if err := c.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
			t.Fatal(err)
		}
		if err := c.QueryRowContext(ctx, `PRAGMA query_only`).Scan(&qo); err != nil {
			t.Fatal(err)
		}
		if fk != 1 || qo != 1 {
			t.Fatalf("reader conn %d: foreign_keys=%d query_only=%d", i, fk, qo)
		}
		c.Close()
	}
}

func TestOpenWriterIsSingleConnection(t *testing.T) {
	db := openTemp(t)
	if got := db.Writer().Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("writer MaxOpenConnections = %d, want 1", got)
	}
}

func TestDSNCarriesRolePragmas(t *testing.T) {
	w := dsn("/x/y.db", roleWriter)
	r := dsn("/x/y.db", roleReader)
	for _, want := range []string{"foreign_keys", "journal_mode", "secure_delete", "auto_vacuum", "_txlock=immediate"} {
		if !strings.Contains(w, want) {
			t.Errorf("writer DSN lacks %s: %s", want, w)
		}
	}
	if !strings.Contains(r, "query_only") || strings.Contains(r, "_txlock") {
		t.Errorf("reader DSN wrong: %s", r)
	}
	if !strings.HasPrefix(w, "/x/y.db?") {
		t.Errorf("DSN must be a plain path, not a URI: %s", w)
	}
}

func TestOpenCreatesMissingDataDir(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a", "b", "c.db")
	db, err := Open(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	closeDB(t, db)
}
