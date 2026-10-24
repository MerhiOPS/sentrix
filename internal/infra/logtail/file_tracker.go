package logtail

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// maxReadPerPoll bounds one file's read in one poll, so a burst cannot make
// a single batch unbounded; the rest is read on the next poll.
const maxReadPerPoll = 4 << 20

// tracker follows one file: an open handle retained across polls, so when
// the path is rotated the old file can still be drained to its end before
// the new one is read (OLD C-C2).
type tracker struct {
	path   string
	key    string
	f      *os.File
	offset int64 // bytes consumed from the file (including a held partial line)
	sp     splitter
}

func openTracker(path string, start int64, fromEnd bool) (*tracker, error) {
	f, err := openShared(path)
	if err != nil {
		return nil, err
	}
	key, err := fileKey(f)
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	if fromEnd {
		fi, err := f.Stat()
		if err != nil {
			return nil, errors.Join(err, f.Close())
		}
		start = fi.Size()
	}
	return &tracker{path: path, key: key, f: f, offset: start}, nil
}

// read returns complete lines appended since the last read. A file shorter
// than the offset was truncated in place (copytruncate): reading restarts at
// 0. Lines written between the copy and the truncate are lost to every
// tailer — copytruncate is best effort by nature, and documented as such.
func (t *tracker) read() ([]string, error) {
	fi, err := t.f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", t.path, err)
	}
	if fi.Size() < t.offset {
		t.offset, t.sp = 0, splitter{}
	}
	n := min(fi.Size()-t.offset, maxReadPerPoll)
	if n <= 0 {
		return nil, nil
	}
	buf := make([]byte, n)
	got, err := t.f.ReadAt(buf, t.offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read %s: %w", t.path, err)
	}
	t.offset += int64(got)
	return t.sp.feed(buf[:got]), nil
}

// drain reads everything left in the (possibly already rotated) file.
func (t *tracker) drain() []string {
	var out []string
	for {
		lines, err := t.read()
		out = append(out, lines...)
		if err != nil || len(lines) == 0 {
			break
		}
	}
	if t.sp.pending() > 0 { // the old file is finished: its last line has no newline coming
		out = append(out, cleanLine(t.sp.partial))
		t.sp = splitter{}
	}
	return out
}

// committedOffset is the position of the last complete line handed out: a
// held partial line is re-read after a restart, never skipped.
func (t *tracker) committedOffset() int64 { return t.offset - int64(t.sp.pending()) }

func (t *tracker) close() error { return t.f.Close() }
