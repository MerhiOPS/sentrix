// Package logtail reads logs on the host: files, journald, Windows Event
// Log. Every source separates its *read* position (advanced as lines are
// handed to the agent) from its *committed* position (advanced only when the
// server acknowledges the batch containing them). Only the committed one is
// persisted, so a crash re-sends lines and never loses them (R1,
// at-least-once).
package logtail

import (
	"context"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Mark is a source-specific read position. The agent stores the latest
// mark of each source with the batch it sealed, and hands it back to Commit
// when that batch is acknowledged.
type Mark any

// Source is one log source.
type Source interface {
	// Name identifies the source in state files and logs; stable across runs.
	Name() string
	// Poll returns new lines since the last poll and the mark after them.
	Poll(ctx context.Context) ([]domain.LogLine, Mark, error)
	// Commit makes m the durable position and returns the state to persist.
	Commit(m Mark) ([]byte, error)
	// Restore loads persisted state (nil: first run).
	Restore(state []byte) error
	// Close releases handles.
	Close() error
}
