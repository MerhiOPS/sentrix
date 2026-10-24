package logtail

import (
	"strings"
	"testing"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

func TestSplitterHoldsPartialLines(t *testing.T) {
	var s splitter
	if got := s.feed([]byte("one\ntw")); len(got) != 1 || got[0] != "one" || s.pending() != 2 {
		t.Fatalf("got %q pending %d", got, s.pending())
	}
	if got := s.feed([]byte("o\r\nthree\n")); len(got) != 2 || got[0] != "two" || got[1] != "three" {
		t.Fatalf("got %q (CR must be trimmed)", got)
	}
}

func TestSplitterCutsOverlongLineAndCleansUTF8(t *testing.T) {
	var s splitter
	got := s.feed([]byte(strings.Repeat("x", domain.MaxMessageBytes+10)))
	if len(got) != 1 || len(got[0]) != domain.MaxMessageBytes || s.pending() != 10 {
		t.Fatalf("overlong: %d lines, first %d bytes, pending %d", len(got), len(got[0]), s.pending())
	}
	got = (&splitter{}).feed([]byte("bad \xff byte\n"))
	if got[0] != "bad � byte" {
		t.Fatalf("invalid UTF-8: %q", got[0])
	}
}

func TestJoinerMergesContinuations(t *testing.T) {
	var j joiner
	var out []string
	for _, l := range []string{"panic: boom", "\tat main.go:10", "  at lib.go:5", "next line"} {
		out = append(out, j.add(l)...)
	}
	out = append(out, j.flush()...)
	if len(out) != 2 || out[0] != "panic: boom\n\tat main.go:10\n  at lib.go:5" || out[1] != "next line" {
		t.Fatalf("joined: %q", out)
	}
}
