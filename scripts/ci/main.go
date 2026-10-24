// Command ci runs the repository checks that CLAUDE.md says are enforced by
// tooling rather than review: file length, import layering, SQL placement
// and platform imports.
//
//	go run ./scripts/ci all
//	go run ./scripts/ci lines|layering|rawsql|platform
package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type check func(root, module string) ([]Violation, error)

var checks = map[string]check{
	"lines":    func(root, _ string) ([]Violation, error) { return checkLines(root) },
	"layering": checkLayering,
	"rawsql":   func(root, _ string) ([]Violation, error) { return checkRawSQL(root) },
	"platform": func(root, _ string) ([]Violation, error) { return checkPlatform(root) },
}

var order = []string{"lines", "layering", "rawsql", "platform"}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: ci all|"+strings.Join(order, "|"))
		os.Exit(2)
	}
	n, err := run(".", os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "ci:", err)
		os.Exit(2)
	}
	if n > 0 {
		fmt.Fprintf(os.Stderr, "ci: %d violation(s)\n", n)
		os.Exit(1)
	}
	fmt.Println("ci: ok")
}

// run executes one named check or all of them and returns the violation count.
func run(root, name string) (int, error) {
	module, err := modulePath(filepath.Join(root, "go.mod"))
	if err != nil {
		return 0, err
	}
	names := []string{name}
	if name == "all" {
		names = order
	}
	total := 0
	for _, n := range names {
		c, ok := checks[n]
		if !ok {
			return 0, fmt.Errorf("unknown check %q", n)
		}
		vs, err := c(root, module)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", n, err)
		}
		total += report(os.Stdout, vs)
	}
	return total, nil
}

func modulePath(goMod string) (string, error) {
	f, err := os.Open(goMod)
	if err != nil {
		return "", fmt.Errorf("read module path: %w", err)
	}
	defer func() { _ = f.Close() }() // read-only: nothing to lose
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("read module path: %w", err)
	}
	return "", errors.New("no module directive in " + goMod)
}
