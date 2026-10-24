package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// The WAL file format: walMagic, one JSON header line, then the batch body
// exactly as it will be sent.

func walSeq(name string) (uint64, bool) {
	base, ok := strings.CutSuffix(name, walExt)
	if !ok || len(base) != 20 {
		return 0, false
	}
	seq, err := strconv.ParseUint(base, 10, 64)
	return seq, err == nil
}

func encodeWAL(p Pending, now time.Time) ([]byte, error) {
	h, err := json.Marshal(walHeader{CreatedMS: now.UnixMilli(), Samples: p.Samples})
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.Grow(len(walMagic) + len(h) + 1 + len(p.Body))
	b.WriteString(walMagic)
	b.Write(h)
	b.WriteByte('\n')
	b.Write(p.Body)
	return b.Bytes(), nil
}

// readHeader parses magic and header from r.
func readHeader(r *bufio.Reader) (walHeader, error) {
	var h walHeader
	magic := make([]byte, len(walMagic))
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != walMagic {
		return h, errors.New("not a wal file")
	}
	line, err := r.ReadBytes('\n')
	if err != nil {
		return h, errors.New("truncated wal header")
	}
	return h, json.Unmarshal(line, &h)
}

func readEntry(path string, seq uint64) (walEntry, error) {
	f, err := os.Open(path) //nolint:gosec // path is built from the state dir and a parsed seq
	if err != nil {
		return walEntry{}, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return walEntry{}, err
	}
	h, err := readHeader(bufio.NewReader(f))
	if err != nil {
		return walEntry{}, err
	}
	return walEntry{seq: seq, size: st.Size(), created: time.UnixMilli(h.CreatedMS), samples: h.Samples}, nil
}

func readBody(path string) ([]byte, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is built from the state dir and a parsed seq
	if err != nil {
		return nil, err
	}
	r := bufio.NewReader(bytes.NewReader(data))
	if _, err := readHeader(r); err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}
