package logtail

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// FileSource tails files matching an allowlist of globs.
type FileSource struct {
	name      string
	globs     []string
	now       func() time.Time
	files     map[string]*tracker // by path
	committed map[string]filePos  // durable positions, by path
	drained   map[string]int64    // this poll: file key → offset it was drained to
	started   bool                // first poll done: later files are new, read from 0
	restored  bool                // state existed: positions are known
}

// NewFileSource tails the given glob patterns.
func NewFileSource(name string, globs []string) (*FileSource, error) {
	for _, g := range globs {
		if _, err := filepath.Match(g, ""); err != nil {
			return nil, fmt.Errorf("bad glob %q: %w", g, err)
		}
	}
	return &FileSource{name: name, globs: globs, now: time.Now, files: map[string]*tracker{}, committed: map[string]filePos{}}, nil
}

// Name implements Source.
func (s *FileSource) Name() string { return s.name }

// Restore implements Source.
func (s *FileSource) Restore(state []byte) error {
	if state == nil {
		return nil
	}
	pos, err := decodeFileState(state)
	if err != nil {
		return err
	}
	s.committed, s.restored = pos, true
	return nil
}

// Poll implements Source.
func (s *FileSource) Poll(_ context.Context) ([]domain.LogLine, Mark, error) {
	paths, err := s.match()
	if err != nil {
		return nil, nil, err
	}
	var lines []domain.LogLine
	var errs []error
	seen := map[string]bool{}
	s.drained = map[string]int64{}
	for _, p := range paths {
		seen[p] = true
		got, err := s.pollPath(p)
		if err != nil {
			errs = append(errs, err)
		}
		lines = append(lines, got...)
	}
	for p, t := range s.files { // gone from the globs (deleted or renamed away): finish, then forget
		if !seen[p] {
			lines = append(lines, s.entries(t.path, t.drain())...)
			errs = append(errs, t.close())
			delete(s.files, p)
		}
	}
	s.started = true
	return lines, s.mark(), errors.Join(errs...)
}

func (s *FileSource) pollPath(p string) ([]domain.LogLine, error) {
	key, err := pathKey(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil // vanished between glob and stat: next poll sorts it out
	}
	if err != nil {
		return nil, fmt.Errorf("identify %s: %w", p, err) // e.g. permission denied: say so, every poll
	}
	var out []domain.LogLine
	t := s.files[p]
	if t != nil && t.key != key { // rotated: drain the old file before the new one
		out = append(out, s.entries(p, t.drain())...)
		s.drained[t.key] = t.offset
		_ = t.close()
		t = nil
	}
	if t == nil {
		if t, err = s.open(p, key); err != nil {
			return out, err
		}
		s.files[p] = t
	}
	raw, err := t.read()
	return append(out, s.entries(p, raw)...), err
}

// open starts tracking p. A file is matched by identity, not path, so a
// rotation that renames app.log → app.log.1 (both matched by the globs)
// continues where it was instead of re-reading it from 0:
//   - drained this poll under another path: continue at that offset;
//   - committed under any path (a restart after rotation): that offset;
//   - first run, never seen: at the end (no history flood);
//   - appeared later: from the start (a rotation's new file).
func (s *FileSource) open(p, key string) (*tracker, error) {
	if off, ok := s.drained[key]; ok {
		return openTracker(p, off, false)
	}
	if c, ok := s.committed[p]; ok && c.Key == key {
		return openTracker(p, c.Offset, false)
	}
	for _, c := range s.committed {
		if c.Key == key {
			return openTracker(p, c.Offset, false)
		}
	}
	return openTracker(p, 0, !s.started && !s.restored)
}

func (s *FileSource) match() ([]string, error) {
	set := map[string]bool{}
	for _, g := range s.globs {
		m, err := filepath.Glob(g)
		if err != nil {
			return nil, err
		}
		for _, p := range m {
			set[p] = true
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func (s *FileSource) entries(path string, raw []string) []domain.LogLine {
	var j joiner
	var msgs []string
	for _, r := range raw {
		msgs = append(msgs, j.add(r)...)
	}
	msgs = append(msgs, j.flush()...)
	now := s.now().UTC()
	out := make([]domain.LogLine, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, domain.LogLine{TS: now, Source: path, Level: guessLevel(m), Message: m})
	}
	return out
}

func (s *FileSource) mark() fileMark {
	m := fileMark{}
	for p, t := range s.files {
		m[p] = filePos{Key: t.key, Offset: t.committedOffset()}
	}
	return m
}

// Commit implements Source.
func (s *FileSource) Commit(m Mark) ([]byte, error) {
	fm, ok := m.(fileMark)
	if !ok {
		return nil, fmt.Errorf("file source %s: foreign mark %T", s.name, m)
	}
	s.committed = map[string]filePos(fm)
	return encodeFileState(fm)
}

// Close implements Source.
func (s *FileSource) Close() error {
	var errs []error
	for _, t := range s.files {
		errs = append(errs, t.close())
	}
	return errors.Join(errs...)
}
