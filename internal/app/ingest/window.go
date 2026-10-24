package ingest

import (
	"sort"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Window is the accepted timestamp range relative to the server clock.
type Window struct {
	// MaxAge is the oldest sample accepted. It is also how long batch_seq
	// records are kept (×2): a pruned seq can never be re-accepted with data,
	// because its samples would be rejected by this clamp (PLAN §4).
	MaxAge time.Duration
	// MaxFuture tolerates small agent clock skew.
	MaxFuture time.Duration
}

// DefaultWindow is the production window.
var DefaultWindow = Window{MaxAge: 72 * time.Hour, MaxFuture: 60 * time.Second}

// seqRetentionFactor: batch_seq records outlive the window by this factor.
const seqRetentionFactor = 2

// SeqRetention is how long replay records must be kept for this window.
func (w Window) SeqRetention() time.Duration { return seqRetentionFactor * w.MaxAge }

// accept reports whether t is inside the window at now.
func (w Window) accept(t, now time.Time) bool {
	return !t.Before(now.Add(-w.MaxAge)) && !t.After(now.Add(w.MaxFuture))
}

// normalize validates, clamps and snaps samples, then dedupes them by
// snapped timestamp (last one wins, as the most recent reading of that slot)
// and sorts them. It returns the survivors and how many were rejected.
func normalize(in []domain.CoreSample, interval time.Duration, w Window, now time.Time) ([]domain.CoreSample, int) {
	bySlot := make(map[int64]int, len(in))
	out := make([]domain.CoreSample, 0, len(in))
	rejected := 0
	for _, s := range in {
		if s.Validate() != nil || !w.accept(s.TS, now) {
			rejected++
			continue
		}
		s.TS = domain.SnapToGrid(s.TS, interval)
		slot := s.TS.UnixMilli()
		if i, dup := bySlot[slot]; dup {
			out[i] = s
			continue
		}
		bySlot[slot] = len(out)
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TS.Before(out[j].TS) })
	return out, rejected
}
