// Package livehub is the in-process pub/sub feeding the dashboard's
// WebSocket. It is fed from the ingest path after commit and never touches
// the database: a hundred open dashboards cost memory, not queries.
package livehub

import (
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// ring holds the most recent samples of one host, for reconnect backfill.
type ring struct {
	buf  []domain.CoreSample
	next int
	full bool
}

func newRing(size int) *ring { return &ring{buf: make([]domain.CoreSample, size)} }

func (r *ring) push(s domain.CoreSample) {
	r.buf[r.next] = s
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
}

// since returns samples newer than t, oldest first, at most limit of them.
// It walks newest-first and stops at t or limit, so a reconnect costs
// O(limit) and never copies the whole ring (OLD B6: a whole-ring copy per
// subscribe, under the ingest lock, was 7 MB per keystroke).
func (r *ring) since(t time.Time, limit int) []domain.CoreSample {
	n := r.next
	if r.full {
		n = len(r.buf)
	}
	out := make([]domain.CoreSample, 0, min(limit, n))
	for k := 1; k <= n && len(out) < limit; k++ {
		s := r.buf[(r.next-k+len(r.buf))%len(r.buf)]
		if !s.TS.After(t) {
			break
		}
		out = append(out, s)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
