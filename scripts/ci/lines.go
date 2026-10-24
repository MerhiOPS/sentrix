package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

const maxLines = 300

var lineExts = []string{".go", ".ts", ".tsx", ".sql"}

// exemptSuffixes are generated or test files, which are not hand-maintained
// to the limit.
var exemptSuffixes = []string{
	"_test.go", "_gen.go", ".pb.go",
	".test.ts", ".test.tsx", ".gen.ts", ".d.ts",
}

// checkLines enforces the 300-line limit from CLAUDE.md.
func checkLines(root string) ([]Violation, error) {
	var vs []Violation
	err := walkFiles(root, lineExts, func(abs, rel string) error {
		if isExempt(rel) {
			return nil
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return err
		}
		if n := countLines(data); n > maxLines {
			vs = append(vs, Violation{
				Check: "lines", Path: rel,
				Msg: fmt.Sprintf("%d lines, limit is %d", n, maxLines),
			})
		}
		return nil
	})
	return vs, err
}

func isExempt(rel string) bool {
	for _, s := range exemptSuffixes {
		if strings.HasSuffix(rel, s) {
			return true
		}
	}
	return false
}

// countLines counts lines the way an editor shows them: a final line without
// a trailing newline still counts.
func countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	n := bytes.Count(data, []byte("\n"))
	if data[len(data)-1] != '\n' {
		n++
	}
	return n
}
