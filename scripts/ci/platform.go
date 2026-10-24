package main

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

const windowsSys = "golang.org/x/sys/windows"

// checkPlatform fails when a file imports golang.org/x/sys/windows (or a
// subpackage) without being restricted to windows by file name or build
// constraint. A Linux binary must not carry Windows syscalls (CLAUDE.md, Go).
func checkPlatform(root string) ([]Violation, error) {
	var vs []Violation
	fset := token.NewFileSet()
	err := walkFiles(root, []string{".go"}, func(abs, rel string) error {
		f, err := parser.ParseFile(fset, abs, nil, parser.ImportsOnly|parser.ParseComments)
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if p != windowsSys && !strings.HasPrefix(p, windowsSys+"/") {
				continue
			}
			if windowsOnlyName(rel) || windowsOnlyTag(f.Comments) {
				continue
			}
			vs = append(vs, Violation{
				Check: "platform", Path: rel, Line: fset.Position(imp.Pos()).Line,
				Msg: "imports " + p + " without a windows build constraint",
			})
		}
		return nil
	})
	return vs, err
}

func windowsOnlyName(rel string) bool {
	base := strings.TrimSuffix(strings.TrimSuffix(rel, ".go"), "_test")
	return strings.HasSuffix(base, "_windows")
}

// windowsOnlyTag reports whether a //go:build line exists that is false for
// every GOOS other than windows.
func windowsOnlyTag(groups []*ast.CommentGroup) bool {
	for _, g := range groups {
		for _, c := range g.List {
			if !constraint.IsGoBuild(c.Text) {
				continue
			}
			expr, err := constraint.Parse(c.Text)
			if err != nil {
				return false
			}
			return expr.Eval(func(tag string) bool { return tag == "windows" }) &&
				!expr.Eval(func(tag string) bool { return tag == "linux" || tag == "unix" })
		}
	}
	return false
}
