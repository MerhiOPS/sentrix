package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/MerhiOPS/sentrix/internal/infra/logtail"
)

// WAL limits.
const (
	// DefaultWALBytes bounds the backlog on disk. At ~2 KiB per 10 s batch it
	// holds days; logs fill it faster, which is why it is bytes, not batches.
	DefaultWALBytes = 100 << 20
	// DefaultWALAge drops batches the server would refuse anyway: its window
	// accepts samples up to 72 h old, less an hour of margin for skew and the
	// time a replay takes.
	DefaultWALAge = 71 * time.Hour
	walDirName    = "wal"
	walExt        = ".batch"
	walMagic      = "STXW1\n"
)

// Pending is an encoded batch waiting to be acknowledged. The body is built
// once (gzip JSON) so a resend is byte-identical — the server's replay gate
// keys on batch_seq, and the content must match the seq it was given.
type Pending struct {
	Seq     uint64
	Body    []byte
	Samples int
	// Marks are the log read positions this batch completes, by source.
	// They are committed once the batch is in the WAL (it is then resent
	// until acknowledged or dropped, and a drop is counted); they are not
	// stored in it.
	Marks map[string]logtail.Mark
}

// walHeader is the first line of a WAL file, after the magic.
type walHeader struct {
	CreatedMS int64 `json:"created_ms"`
	Samples   int   `json:"samples"`
}

type walEntry struct {
	seq     uint64
	size    int64
	created time.Time
	samples int
}

// WAL is the agent's on-disk FIFO of sealed batches: one file per batch,
// written atomically, so a crash loses at most the batch being written and a
// restart resends what was not acknowledged — with the same batch_seq, which
// the server deduplicates. Bounded by bytes (drop oldest, counted) and by age.
type WAL struct {
	mu       sync.Mutex
	dir      string
	maxBytes int64
	maxAge   time.Duration
	now      func() time.Time
	entries  []walEntry // ordered by seq: the order batches were sealed
	bytes    int64
	dropped  uint64
	failures uint64 // writes that failed (each also counted as dropped)
	ready    chan struct{}
}

// OpenWAL loads the WAL in stateDir, discarding leftovers of interrupted
// writes and files it cannot read (each counted as dropped).
func OpenWAL(stateDir string, maxBytes int64, maxAge time.Duration, now func() time.Time) (*WAL, error) {
	w := &WAL{dir: filepath.Join(stateDir, walDirName), maxBytes: maxBytes, maxAge: maxAge, now: now,
		ready: make(chan struct{}, 1)}
	if err := os.MkdirAll(w.dir, 0o700); err != nil {
		return nil, fmt.Errorf("create wal dir: %w", err)
	}
	names, err := os.ReadDir(w.dir)
	if err != nil {
		return nil, fmt.Errorf("read wal dir: %w", err)
	}
	for _, d := range names {
		path := filepath.Join(w.dir, d.Name())
		seq, ok := walSeq(d.Name())
		if !ok {
			_ = os.Remove(path) // a temp file from an interrupted write
			continue
		}
		e, err := readEntry(path, seq)
		if err != nil {
			_ = os.Remove(path)
			w.dropped++
			continue
		}
		w.entries = append(w.entries, e)
		w.bytes += e.size
	}
	sort.Slice(w.entries, func(i, j int) bool { return w.entries[i].seq < w.entries[j].seq })
	if len(w.entries) > 0 {
		w.ready <- struct{}{}
	}
	return w, nil
}

// Push writes a batch. When the WAL is full the oldest batches are dropped
// (counted) to make room: losing the oldest data beats refusing new data. A
// failed write is counted as a drop and returned, so the caller does not
// advance log positions for lines that were not kept.
func (w *WAL) Push(p Pending) error {
	data, err := encodeWAL(p, w.now())
	if err != nil {
		return err
	}
	w.mu.Lock()
	size := int64(len(data))
	if size > w.maxBytes {
		w.dropped++
		w.mu.Unlock()
		return fmt.Errorf("batch of %d bytes exceeds the WAL size %d", size, w.maxBytes)
	}
	for len(w.entries) > 0 && w.bytes+size > w.maxBytes {
		w.dropHeadLocked()
	}
	err = writeFileAtomic(w.path(p.Seq), data, 0o600)
	if err != nil {
		w.dropped++
		w.failures++
		w.mu.Unlock()
		return fmt.Errorf("write wal: %w", err)
	}
	w.entries = append(w.entries, walEntry{seq: p.Seq, size: size, created: w.now(), samples: p.Samples})
	w.bytes += size
	w.mu.Unlock()
	select {
	case w.ready <- struct{}{}:
	default:
	}
	return nil
}

// Head returns the oldest batch, first dropping (and counting) batches past
// the age limit and files that can no longer be read.
func (w *WAL) Head() (Pending, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for len(w.entries) > 0 {
		e := w.entries[0]
		if w.now().Sub(e.created) > w.maxAge {
			w.dropHeadLocked()
			continue
		}
		body, err := readBody(w.path(e.seq))
		if err != nil {
			w.dropHeadLocked()
			continue
		}
		return Pending{Seq: e.seq, Body: body, Samples: e.samples}, true
	}
	return Pending{}, false
}

// Remove deletes the head if it is still seq (an acknowledged batch).
func (w *WAL) Remove(seq uint64) { w.removeHead(seq, false) }

// Discard deletes the head and counts it dropped (permanently refused).
func (w *WAL) Discard(seq uint64) { w.removeHead(seq, true) }

func (w *WAL) removeHead(seq uint64, count bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.entries) == 0 || w.entries[0].seq != seq {
		return // dropped by overflow or age while in flight
	}
	w.unlinkHeadLocked()
	if count {
		w.dropped++
	}
}

func (w *WAL) dropHeadLocked() {
	w.unlinkHeadLocked()
	w.dropped++
}

func (w *WAL) unlinkHeadLocked() {
	e := w.entries[0]
	_ = os.Remove(w.path(e.seq)) // a file left behind is re-read and resent: duplicates are deduped
	w.entries = w.entries[1:]
	w.bytes -= e.size
}

// Len is the number of batches waiting.
func (w *WAL) Len() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.entries)
}

// Bytes is the WAL's size on disk.
func (w *WAL) Bytes() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.bytes
}

// Dropped is the number of batches dropped since start: overflow, age,
// unreadable files, failed writes and permanent refusals.
func (w *WAL) Dropped() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.dropped
}

// WriteFailures is the number of batches that could not be written.
func (w *WAL) WriteFailures() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.failures
}

// Ready is signalled when a batch is pushed.
func (w *WAL) Ready() <-chan struct{} { return w.ready }

func (w *WAL) path(seq uint64) string {
	return filepath.Join(w.dir, fmt.Sprintf("%020d%s", seq, walExt))
}
