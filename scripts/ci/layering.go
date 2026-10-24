package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"strings"
)

// checkLayering enforces the DDD import direction: every import from a
// layered package must be stdlib, an allowed module-internal package, or (for
// infra and agent only) third-party.
func checkLayering(root, module string) ([]Violation, error) {
	var vs []Violation
	fset := token.NewFileSet()
	err := walkFiles(root, []string{".go"}, func(abs, rel string) error {
		l := layerFor(path.Dir(rel))
		if l == nil {
			return nil
		}
		f, err := parser.ParseFile(fset, abs, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return fmt.Errorf("%s: bad import %s: %w", rel, imp.Path.Value, err)
			}
			if msg := judgeImport(l, module, p); msg != "" {
				vs = append(vs, Violation{
					Check: "layering", Path: rel,
					Line: fset.Position(imp.Pos()).Line, Msg: msg,
				})
			}
		}
		return nil
	})
	return vs, err
}

// judgeImport returns "" when l may import p, otherwise the reason it may not.
func judgeImport(l *layer, module, p string) string {
	if isStdlib(p) {
		return ""
	}
	if p == module || strings.HasPrefix(p, module+"/") {
		rel := strings.TrimPrefix(strings.TrimPrefix(p, module), "/")
		if l.allows(rel) {
			return ""
		}
		return fmt.Sprintf("%s must not import %s", l.name, rel)
	}
	if l.thirdParty {
		return ""
	}
	return fmt.Sprintf("%s must not import third-party package %s", l.name, p)
}
