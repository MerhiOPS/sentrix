package sqlite

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// scanChunk is how much of a file is read at a time.
const scanChunk = 1 << 20

// scanFiles byte-scans the database and its WAL for each needle and
// describes every hit. It reads the files, not the tables: deleted content
// in free pages, old WAL frames and index pages is exactly what it is for.
func scanFiles(dbPath string, needles [][]byte) ([]string, error) {
	var hits []string
	for _, p := range []string{dbPath, dbPath + "-wal"} {
		found, err := scanFile(p, needles)
		if errors.Is(err, os.ErrNotExist) {
			continue // no WAL after a TRUNCATE checkpoint is fine
		}
		if err != nil {
			return nil, err
		}
		for i, off := range found {
			if off >= 0 {
				hits = append(hits, fmt.Sprintf("%s: needle %d at byte %d", filepath.Base(p), i, off))
			}
		}
	}
	return hits, nil
}

// scanFile returns, per needle, the first offset where it occurs or -1.
func scanFile(path string, needles [][]byte) ([]int64, error) {
	f, err := os.Open(path) //nolint:gosec // the store's own database file
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	longest := 0
	for _, n := range needles {
		longest = max(longest, len(n))
	}
	found := make([]int64, len(needles))
	for i := range found {
		found[i] = -1
	}
	buf := make([]byte, 0, scanChunk+longest)
	chunk := make([]byte, scanChunk)
	var base int64 // file offset of buf[0]
	for {
		n, err := f.Read(chunk)
		buf = append(buf, chunk[:n]...)
		for i, nd := range needles {
			if found[i] < 0 && len(nd) > 0 {
				if at := bytes.Index(buf, nd); at >= 0 {
					found[i] = base + int64(at)
				}
			}
		}
		if errors.Is(err, io.EOF) || n == 0 && err == nil {
			return found, nil
		}
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", filepath.Base(path), err)
		}
		// Keep the tail so a needle spanning two chunks is still found.
		keep := min(len(buf), max(longest-1, 0))
		base += int64(len(buf) - keep)
		buf = append(buf[:0], buf[len(buf)-keep:]...)
	}
}
