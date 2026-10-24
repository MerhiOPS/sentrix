package logtail

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

func appendTo(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func poll(t *testing.T, s *FileSource) ([]string, Mark) {
	t.Helper()
	lines, m, err := s.Poll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range lines {
		out = append(out, l.Message)
	}
	return out, m
}

func newSource(t *testing.T, globs ...string) *FileSource {
	t.Helper()
	s, err := NewFileSource("files", globs)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestFirstRunTailsFromEndNewFilesFromStart(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.log")
	appendTo(t, old, "history 1\nhistory 2\n")
	s := newSource(t, filepath.Join(dir, "*.log"))
	if got, _ := poll(t, s); len(got) != 0 {
		t.Fatalf("first run shipped history: %q", got)
	}
	appendTo(t, old, "fresh\n")
	appendTo(t, filepath.Join(dir, "new.log"), "from the start\n")
	got, _ := poll(t, s)
	if strings.Join(got, "|") != "from the start|fresh" && strings.Join(got, "|") != "fresh|from the start" {
		t.Fatalf("got %q", got)
	}
}

// rotate runs the rename-rotation scenario. rotatedMatches decides whether
// the rotated name is also inside the globs (app.log.1 vs app.log.1.bak).
func rotate(t *testing.T, rotatedMatches bool) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	appendTo(t, path, "")
	glob := filepath.Join(dir, "app.log*")
	if !rotatedMatches {
		glob = path
	}
	s := newSource(t, glob)
	poll(t, s)
	var got []string
	for i := 0; i < 3; i++ {
		appendTo(t, path, fmt.Sprintf("before-%d\n", i))
	}
	lines, _ := poll(t, s)
	got = append(got, lines...)
	appendTo(t, path, "between-polls-1\nbetween-polls-2\n") // written, then rotated before the next poll
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	appendTo(t, path, "after-0\nafter-1\n")
	for i := 0; i < 3; i++ {
		lines, _ = poll(t, s)
		got = append(got, lines...)
	}
	want := "before-0 before-1 before-2 between-polls-1 between-polls-2 after-0 after-1"
	if strings.Join(got, " ") != want {
		t.Fatalf("rotation (rotated name in globs: %v)\n got  %v\n want %s", rotatedMatches, got, want)
	}
}

func TestRenameRotationLosesNothingShipsNothingTwice(t *testing.T) {
	t.Run("rotated name outside globs", func(t *testing.T) { rotate(t, false) })
	t.Run("rotated name inside globs", func(t *testing.T) { rotate(t, true) })
}

func TestTruncationRestartsAtZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	appendTo(t, path, "")
	s := newSource(t, path)
	poll(t, s)
	appendTo(t, path, "aaaaaaaaaa\nbbbbbbbbbb\n")
	poll(t, s)
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	appendTo(t, path, "after truncate\n")
	if got, _ := poll(t, s); len(got) != 1 || got[0] != "after truncate" {
		t.Fatalf("after copytruncate: %q", got)
	}
}

// TestRestartResumesFromCommittedNotRead: R1 — only the committed position
// survives a restart, and a held partial line is not skipped.
func TestRestartResumesFromCommittedNotRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	appendTo(t, path, "")
	s := newSource(t, path)
	poll(t, s)
	appendTo(t, path, "acked\n")
	_, m := poll(t, s)
	state, err := s.Commit(m) // the batch with "acked" was acknowledged
	if err != nil {
		t.Fatal(err)
	}
	appendTo(t, path, "read-not-acked\npartial-no-newline")
	poll(t, s) // read, but its batch never acknowledged: crash
	_ = s.Close()

	s2 := newSource(t, path)
	if err := s2.Restore(state); err != nil {
		t.Fatal(err)
	}
	appendTo(t, path, " finished\n")
	got, _ := poll(t, s2)
	if strings.Join(got, "|") != "read-not-acked|partial-no-newline finished" {
		t.Fatalf("after restart: %q (want the unacknowledged lines again, partial line whole)", got)
	}
}

// TestCommittedMarkExcludesHeldPartialLine: a mark taken while a line is
// still incomplete must point before it, or a restart skips its first half.
func TestCommittedMarkExcludesHeldPartialLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	appendTo(t, path, "")
	s := newSource(t, path)
	poll(t, s)
	appendTo(t, path, "whole\nhalf-of-a-")
	_, m := poll(t, s)
	state, _ := s.Commit(m) // acknowledged while "half-of-a-" is held back
	_ = s.Close()
	s2 := newSource(t, path)
	if err := s2.Restore(state); err != nil {
		t.Fatal(err)
	}
	appendTo(t, path, "line\n")
	if got, _ := poll(t, s2); strings.Join(got, "|") != "half-of-a-line" {
		t.Fatalf("after restart: %q, want the partial line whole", got)
	}
}

func TestRestartAfterRenameContinuesByIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	appendTo(t, path, "")
	s := newSource(t, filepath.Join(dir, "app.log*"))
	poll(t, s)
	appendTo(t, path, "one\n")
	_, m := poll(t, s)
	state, _ := s.Commit(m)
	_ = s.Close()
	if err := os.Rename(path, path+".1"); err != nil { // rotated while the agent was down
		t.Fatal(err)
	}
	appendTo(t, path+".1", "two\n")
	s2 := newSource(t, filepath.Join(dir, "app.log*"))
	if err := s2.Restore(state); err != nil {
		t.Fatal(err)
	}
	if got, _ := poll(t, s2); strings.Join(got, "|") != "two" {
		t.Fatalf("renamed while down: %q, want only the new line", got)
	}
}

func TestGuessLevel(t *testing.T) {
	for in, want := range map[string]domain.Level{
		"2026-10-24 ERROR db down":   domain.LevelError,
		"[warn] slow":                domain.LevelWarn,
		"level=info msg=ok":          domain.LevelInfo,
		"ERRORS_TOTAL=3 is a metric": domain.LevelUnknown,
		"no level here":              domain.LevelUnknown,
		"FATAL: out of memory":       domain.LevelFatal,
	} {
		if got := guessLevel(in); got != want {
			t.Errorf("guessLevel(%q) = %s, want %s", in, got, want)
		}
	}
}
