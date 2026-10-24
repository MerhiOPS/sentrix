package main

import (
	"strings"
	"testing"
)

func lines(n int, trailingNewline bool) string {
	s := strings.Repeat("x\n", n)
	if !trailingNewline {
		s = strings.TrimSuffix(s, "\n")
	}
	return s
}

func TestCheckLines(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a/at_limit.go":              lines(300, true),
		"a/over.go":                  lines(301, true),
		"a/over_no_newline.ts":       lines(301, false),
		"a/over_test.go":             lines(500, true),
		"web/src/api/types.gen.ts":   lines(500, true),
		"node_modules/x/huge.ts":     lines(500, true),
		"a/notes.md":                 lines(500, true),
		"migrations/0001_over.sql":   lines(301, true),
		"scripts/testdata/bad.go":    lines(500, true),
		"web/src/App.test.tsx":       lines(500, true),
		"web/src/components/Big.tsx": lines(301, true),
	})
	vs, err := checkLines(root)
	if err != nil {
		t.Fatal(err)
	}
	assertFlagged(t, vs, "a/over.go", "a/over_no_newline.ts",
		"migrations/0001_over.sql", "web/src/components/Big.tsx")
}

func TestCheckLayering(t *testing.T) {
	m := testModule + "/internal/"
	root := writeTree(t, map[string]string{
		"internal/domain/ok.go":            goFile("", "fmt"),
		"internal/domain/up.go":            goFile("", m+"infra/sqlite"),
		"internal/domain/thirdparty.go":    goFile("", "github.com/google/uuid"),
		"internal/app/ok.go":               goFile("", m+"domain", m+"app/ingest"),
		"internal/app/infra.go":            goFile("", m+"infra/sqlite"),
		"internal/app/thirdparty.go":       goFile("", "modernc.org/sqlite"),
		"internal/wire/up.go":              goFile("", m+"domain"),
		"internal/infra/sqlite/ok.go":      goFile("", m+"app", "modernc.org/sqlite"),
		"internal/infra/sqlite/wire.go":    goFile("", m+"wire"),
		"internal/infra/ingesthttp/ok.go":  goFile("", m+"wire", m+"app"),
		"internal/agent/ok.go":             goFile("", m+"wire", m+"infra/collector"),
		"internal/agent/store.go":          goFile("", m+"infra/sqlite"),
		"cmd/sentrix-server/main.go":       goFile("", m+"infra/sqlite", m+"wire"),
		"internal/domainish/free.go":       goFile("", m+"infra/sqlite"),
		"internal/domain/testdata/skip.go": goFile("", m+"infra/sqlite"),
	})
	vs, err := checkLayering(root, testModule)
	if err != nil {
		t.Fatal(err)
	}
	assertFlagged(t, vs,
		"internal/agent/store.go",
		"internal/app/infra.go", "internal/app/thirdparty.go",
		"internal/domain/thirdparty.go", "internal/domain/up.go",
		"internal/infra/sqlite/wire.go",
		"internal/wire/up.go")
}

func TestCheckRawSQL(t *testing.T) {
	src := func(lit string) string { return "package p\n\nvar q = " + lit + "\n" }
	root := writeTree(t, map[string]string{
		"internal/app/select.go":         src("`\n  SELECT id FROM hosts WHERE tenant_id = ?`"),
		"internal/app/insert.go":         src(`"INSERT OR IGNORE INTO t VALUES (1)"`),
		"internal/app/prose.go":          src(`"select a host to continue"`),
		"internal/app/mention.go":        src(`"failed: SELECT returned nothing"`),
		"internal/infra/sqlite/store.go": src(`"SELECT 1"`),
		"internal/infra/sqlite/x/sub.go": src(`"DELETE FROM logs"`),
		"cmd/tool/pragma.go":             src(`"PRAGMA foreign_keys = ON"`),
	})
	vs, err := checkRawSQL(root)
	if err != nil {
		t.Fatal(err)
	}
	assertFlagged(t, vs, "cmd/tool/pragma.go", "internal/app/insert.go", "internal/app/select.go")
}

func TestCheckPlatform(t *testing.T) {
	win := "golang.org/x/sys/windows"
	root := writeTree(t, map[string]string{
		"a/untagged.go":           goFile("", win),
		"a/sub_untagged.go":       goFile("", win+"/svc"),
		"a/named_windows.go":      goFile("", win),
		"a/named_windows_test.go": goFile("", win),
		"a/tagged.go":             goFile("//go:build windows", win),
		"a/linux_too.go":          goFile("//go:build windows || linux", win),
		"a/other.go":              goFile("", "golang.org/x/sys/unix"),
	})
	vs, err := checkPlatform(root)
	if err != nil {
		t.Fatal(err)
	}
	assertFlagged(t, vs, "a/linux_too.go", "a/sub_untagged.go", "a/untagged.go")
}

func TestRunCountsAcrossChecks(t *testing.T) {
	root := writeTree(t, map[string]string{
		"internal/domain/up.go": goFile("", testModule+"/internal/infra/sqlite"),
		"internal/app/q.go":     "package p\n\nvar q = \"SELECT 1\"\n",
	})
	n, err := run(root, "all")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("run(all) = %d violations, want 2", n)
	}
	if _, err := run(root, "nope"); err == nil {
		t.Fatal("unknown check must be an error")
	}
}
