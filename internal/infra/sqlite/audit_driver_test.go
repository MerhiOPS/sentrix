package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"sync"

	modernc "modernc.org/sqlite"
)

// recorder captures every statement text sent through the audit driver.
type recorder struct {
	mu    sync.Mutex
	stmts []string
}

func (r *recorder) add(q string) {
	r.mu.Lock()
	r.stmts = append(r.stmts, q)
	r.mu.Unlock()
}

func (r *recorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.stmts...)
}

var auditRec = &recorder{}

func init() {
	sql.Register("sqlite-audit", auditDriver{inner: &modernc.Driver{}})
}

type auditDriver struct{ inner driver.Driver }

func (d auditDriver) Open(name string) (driver.Conn, error) {
	c, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &auditConn{Conn: c}, nil
}

// auditConn forwards to the modernc connection, recording statement text.
// It implements the context interfaces database/sql prefers, so behaviour
// (including _txlock=immediate in BeginTx) is the real driver's.
type auditConn struct{ driver.Conn }

func (c *auditConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	auditRec.add(q)
	return c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, q)
}

func (c *auditConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	auditRec.add(q)
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
}

func (c *auditConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	auditRec.add(q)
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}

func (c *auditConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
}

func (c *auditConn) Ping(ctx context.Context) error {
	if p, ok := c.Conn.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

func (c *auditConn) ResetSession(ctx context.Context) error {
	if r, ok := c.Conn.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}
