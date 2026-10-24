package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
)

// digest hashes every table (FTS shadow tables included) as "rows:sha256",
// rows ordered by all columns so the result does not depend on page layout.
func digest(ctx context.Context, db *sql.DB) (map[string]string, error) {
	tables, err := listTables(ctx, db)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, t := range tables {
		d, err := digestTable(ctx, db, t)
		if err != nil {
			return nil, err
		}
		out[t] = d
	}
	return out, nil
}

func listTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table'
		AND name NOT LIKE 'sqlite_%' AND sql NOT LIKE 'CREATE VIRTUAL TABLE%' ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		tables = append(tables, n)
	}
	return tables, rows.Err()
}

func digestTable(ctx context.Context, db *sql.DB, table string) (string, error) {
	quoted := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info(?)`, table).Scan(&n); err != nil {
		return "", fmt.Errorf("columns of %s: %w", table, err)
	}
	order := make([]string, n)
	for i := range order {
		order[i] = fmt.Sprint(i + 1)
	}
	//nolint:gosec // table names come from sqlite_master, quoted
	rows, err := db.QueryContext(ctx, `SELECT * FROM `+quoted+` ORDER BY `+strings.Join(order, ", "))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", table, err)
	}
	defer rows.Close()
	h := sha256.New()
	vals := make([]any, n)
	ptrs := make([]any, n)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	count := 0
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return "", err
		}
		for _, v := range vals {
			fmt.Fprintf(h, "%T:%v|", v, v)
		}
		h.Write([]byte{'\n'})
		count++
	}
	return fmt.Sprintf("%d:%s", count, hex.EncodeToString(h.Sum(nil))[:16]), rows.Err()
}
