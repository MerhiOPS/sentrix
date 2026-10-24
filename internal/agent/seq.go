package agent

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const seqFile = "batch_seq"

// SeqSource hands out batch_seq values that never repeat for this host.
//
// Each value is max(last + 1, now in epoch nanoseconds), and is persisted
// before it is returned. The epoch floor is what makes a lost or restored
// state directory safe: a fresh agent starting at 1 would have every new
// batch acked by the server as a "replay" of an old one and silently
// discarded (PLAN §4). Persisting means a clock stepped backwards still
// cannot reuse a value.
type SeqSource struct {
	mu   sync.Mutex
	path string
	last uint64
	now  func() time.Time
}

// OpenSeqSource loads the last value from dir. A missing or unreadable file
// starts from the clock floor, which is safe by construction.
func OpenSeqSource(dir string, now func() time.Time) (*SeqSource, error) {
	s := &SeqSource{path: filepath.Join(dir, seqFile), now: now}
	b, err := os.ReadFile(s.path)
	switch {
	case err == nil:
		if v, perr := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64); perr == nil {
			s.last = v
		}
	case !os.IsNotExist(err):
		return nil, fmt.Errorf("read batch_seq: %w", err)
	}
	return s, nil
}

// Next allocates and persists the next value.
func (s *SeqSource) Next() (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	floor := uint64(max(s.now().UnixNano(), 0))
	next := max(s.last+1, floor)
	if next > math.MaxInt64 {
		return 0, fmt.Errorf("batch_seq exhausted")
	}
	if err := writeFileAtomic(s.path, []byte(strconv.FormatUint(next, 10)+"\n"), 0o600); err != nil {
		return 0, fmt.Errorf("persist batch_seq: %w", err)
	}
	s.last = next
	return next, nil
}
