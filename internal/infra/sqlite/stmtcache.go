package sqlite

import (
	"context"
	"database/sql"
	"sync"
)

// stmtCache prepares each ingest statement once on the writer connection.
// Without it every row of every batch is parsed again: profiling the
// ingest ceiling showed SQLite's parser at ~30 % of server CPU. The set of
// statements is fixed by the code, so the cache is bounded.
//
// It never prepares inside a transaction: the writer pool has one
// connection, held by the transaction, so preparing on the pool would wait
// for itself. A miss runs unprepared and is remembered; learn prepares the
// misses once the transaction has released the connection.
type stmtCache struct {
	db     *sql.DB
	mu     sync.Mutex
	m      map[string]*sql.Stmt
	missed map[string]bool
}

func newStmtCache(db *sql.DB) *stmtCache {
	return &stmtCache{db: db, m: map[string]*sql.Stmt{}, missed: map[string]bool{}}
}

// lookup returns the prepared statement, or nil and records the miss.
func (c *stmtCache) lookup(query string) *sql.Stmt {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, ok := c.m[query]
	if !ok {
		c.missed[query] = true
	}
	return st
}

// learn prepares the statements that missed. Call with no transaction
// open on the writer. A statement that fails to prepare stays unprepared.
func (c *stmtCache) learn(ctx context.Context) {
	c.mu.Lock()
	missed := c.missed
	c.missed = map[string]bool{}
	c.mu.Unlock()
	for q := range missed {
		st, err := c.db.PrepareContext(ctx, q)
		if err != nil {
			continue
		}
		c.mu.Lock()
		if _, dup := c.m[q]; dup {
			_ = st.Close() //nolint:sqlclosecheck // a duplicate prepare, discarded at once; cached ones live as long as the store
		} else {
			c.m[q] = st
		}
		c.mu.Unlock()
	}
}

// batchTx is a writer transaction whose statements come from the cache.
// Tx.StmtContext reuses the statement already prepared on the single writer
// connection, so nothing is parsed twice.
type batchTx struct {
	*sql.Tx
	stmts *stmtCache
}

func (t *batchTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if st := t.stmts.lookup(query); st != nil {
		return t.StmtContext(ctx, st).ExecContext(ctx, args...) //nolint:sqlclosecheck // a tx-bound statement is closed when the tx ends
	}
	return t.Tx.ExecContext(ctx, query, args...)
}

func (t *batchTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if st := t.stmts.lookup(query); st != nil {
		return t.StmtContext(ctx, st).QueryContext(ctx, args...) //nolint:sqlclosecheck // a tx-bound statement is closed when the tx ends
	}
	return t.Tx.QueryContext(ctx, query, args...)
}

func (t *batchTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if st := t.stmts.lookup(query); st != nil {
		return t.StmtContext(ctx, st).QueryRowContext(ctx, args...) //nolint:sqlclosecheck // a tx-bound statement is closed when the tx ends
	}
	return t.Tx.QueryRowContext(ctx, query, args...)
}
