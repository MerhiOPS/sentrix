package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testModule = "example.com/m"

// writeTree materialises files (slash paths → contents) under a temp root
// with a go.mod, and returns the root.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files["go.mod"] = "module " + testModule + "\n\ngo 1.26\n"
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// goFile builds a Go source file with an optional header (build constraint)
// and the given imports.
func goFile(header string, imports ...string) string {
	var b strings.Builder
	if header != "" {
		b.WriteString(header + "\n\n")
	}
	b.WriteString("package p\n\n")
	for _, imp := range imports {
		b.WriteString("import _ \"" + imp + "\"\n")
	}
	return b.String()
}

// paths returns the sorted set of flagged paths, for compact assertions.
func paths(vs []Violation) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range vs {
		if !seen[v.Path] {
			seen[v.Path] = true
			out = append(out, v.Path)
		}
	}
	return out
}

func assertFlagged(t *testing.T, vs []Violation, want ...string) {
	t.Helper()
	got := strings.Join(paths(vs), ",")
	if exp := strings.Join(want, ","); got != exp {
		t.Fatalf("flagged %q, want %q\nviolations: %v", got, exp, vs)
	}
}
