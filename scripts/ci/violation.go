package main

import (
	"fmt"
	"io"
	"sort"
)

// Violation is one rule broken at one place in the tree.
type Violation struct {
	Check string
	Path  string // slash-separated, relative to the scanned root
	Line  int    // 0 when the violation is about the whole file
	Msg   string
}

func (v Violation) String() string {
	if v.Line > 0 {
		return fmt.Sprintf("[%s] %s:%d: %s", v.Check, v.Path, v.Line, v.Msg)
	}
	return fmt.Sprintf("[%s] %s: %s", v.Check, v.Path, v.Msg)
}

// report writes violations in a stable order and returns how many there were.
func report(w io.Writer, vs []Violation) int {
	sort.Slice(vs, func(i, j int) bool {
		if vs[i].Path != vs[j].Path {
			return vs[i].Path < vs[j].Path
		}
		return vs[i].Line < vs[j].Line
	})
	for _, v := range vs {
		fmt.Fprintln(w, v)
	}
	return len(vs)
}
