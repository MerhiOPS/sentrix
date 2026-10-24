package agent

import "sync"

// Pending is an encoded batch waiting to be acknowledged. The body is built
// once (gzip JSON) so a resend is byte-identical — the server's replay gate
// keys on batch_seq, and the content must match the seq it was given.
type Pending struct {
	Seq     uint64
	Body    []byte
	Samples int
}

// Buffer is a bounded FIFO of pending batches (in memory in Phase 1; the
// disk WAL replaces it in Phase 6). When full, the oldest batch is dropped
// and counted: losing the oldest data is better than refusing new data.
type Buffer struct {
	mu      sync.Mutex
	max     int
	items   []Pending
	dropped uint64
	ready   chan struct{} // signalled (non-blocking) on every push
}

// NewBuffer makes a buffer holding at most max batches.
func NewBuffer(max int) *Buffer {
	return &Buffer{max: max, ready: make(chan struct{}, 1)}
}

// Push appends a batch, dropping the oldest if the buffer is full.
func (b *Buffer) Push(p Pending) {
	b.mu.Lock()
	if len(b.items) >= b.max {
		b.items = b.items[1:]
		b.dropped++
	}
	b.items = append(b.items, p)
	b.mu.Unlock()
	select {
	case b.ready <- struct{}{}:
	default:
	}
}

// Head returns the oldest batch without removing it.
func (b *Buffer) Head() (Pending, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.items) == 0 {
		return Pending{}, false
	}
	return b.items[0], true
}

// Remove deletes the head if it is still seq — it may already have been
// dropped by overflow while it was in flight.
func (b *Buffer) Remove(seq uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.items) > 0 && b.items[0].Seq == seq {
		b.items = b.items[1:]
	}
}

// Discard removes the head and counts it as dropped (a permanently invalid
// batch the server refused).
func (b *Buffer) Discard(seq uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.items) > 0 && b.items[0].Seq == seq {
		b.items = b.items[1:]
		b.dropped++
	}
}

// Len is the number of batches waiting.
func (b *Buffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.items)
}

// Dropped is the total number of batches dropped since start.
func (b *Buffer) Dropped() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dropped
}

// Ready is signalled when a batch is pushed.
func (b *Buffer) Ready() <-chan struct{} { return b.ready }
