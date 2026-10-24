package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"strconv"
)

// sqlAllowed is the only directory where SQL text may appear in Go code.
const sqlAllowed = "internal/infra/sqlite"

// sqlStart matches a string literal that begins like a SQL statement. Only
// the start is checked, and keywords must be upper case (the repo convention),
// so prose that merely mentions "select" is not SQL.
var sqlStart = regexp.MustCompile(`(?s)^\s*(` +
	`SELECT\s|INSERT\s+(OR\s+\w+\s+)?INTO\s|UPDATE\s+\w+\s+SET\s|DELETE\s+FROM\s|` +
	`REPLACE\s+INTO\s|WITH\s+\w+\s+AS\s*\(|CREATE\s+(TABLE|INDEX|UNIQUE|VIRTUAL|TRIGGER|VIEW)\s|` +
	`DROP\s+(TABLE|INDEX|VIEW|TRIGGER)\s|ALTER\s+TABLE\s|PRAGMA\s)`)

// checkRawSQL fails on SQL string literals outside the SQLite adapter.
func checkRawSQL(root string) ([]Violation, error) {
	var vs []Violation
	fset := token.NewFileSet()
	err := walkFiles(root, []string{".go"}, func(abs, rel string) error {
		if underPrefix(path.Dir(rel), sqlAllowed) {
			return nil
		}
		f, err := parser.ParseFile(fset, abs, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err == nil && sqlStart.MatchString(s) {
				vs = append(vs, Violation{
					Check: "rawsql", Path: rel,
					Line: fset.Position(lit.Pos()).Line,
					Msg:  "SQL outside " + sqlAllowed,
				})
			}
			return true
		})
		return nil
	})
	return vs, err
}
