package ingest

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Shed reasons reported to the agent and metered.
const (
	ShedQuota    = "quota"
	ShedDisk     = "disk"
	ShedDisabled = "disabled" // the server has no redaction configured
)

// normalizeLogs validates and clamps log lines (clamped, never snapped: a
// log line's time is when it happened, not a sampling slot) and orders them
// by time, keeping arrival order for equal timestamps.
func normalizeLogs(in []domain.LogLine, w Window, now time.Time) ([]domain.LogLine, int) {
	out := make([]domain.LogLine, 0, len(in))
	rejected := 0
	for _, l := range in {
		if l.Validate() != nil || !w.accept(l.TS, now) {
			rejected++
			continue
		}
		l.TS = l.TS.UTC()
		out = append(out, l)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TS.Before(out[j].TS) })
	return out, rejected
}

// prepareLogs validates, redacts and applies the disk watermark — everything
// that does not need the transaction's state. The budget is applied in build.
func (s *Service) prepareLogs(ctx context.Context, sc domain.Scope, in []domain.LogLine, now time.Time) ([]domain.LogLine, LogStats, int, error) {
	if len(in) == 0 {
		return nil, LogStats{}, 0, nil
	}
	lines, rejected := normalizeLogs(in, s.window, now)
	stats := LogStats{Received: len(in)}
	if s.redact == nil {
		// Never store unredacted logs — and never fail the batch either: a
		// failed batch would cost the host its metrics too. Shed, metered.
		stats.Shed, stats.ShedReason = len(lines), ShedDisabled
		return nil, stats, rejected, nil
	}
	kept, redacted, dropped, err := s.redact.Redact(ctx, sc, lines)
	if err != nil {
		return nil, LogStats{}, 0, fmt.Errorf("redact: %w", err)
	}
	stats.Redacted, stats.Dropped = redacted, dropped
	if s.disk != nil && !s.disk.LogsAllowed() && len(kept) > 0 {
		stats.Shed, stats.ShedReason = len(kept), ShedDisk
		kept = nil
	}
	return kept, stats, rejected, nil
}

// fitBudget keeps lines, in order, while their bytes fit the budget; the
// rest are shed for quota. A line is never split.
func fitBudget(lines []domain.LogLine, budget int64) ([]domain.LogLine, int, int64) {
	var used int64
	for i, l := range lines {
		sz := int64(l.Size())
		if used+sz > budget {
			return lines[:i], len(lines) - i, used
		}
		used += sz
	}
	return lines, 0, used
}
