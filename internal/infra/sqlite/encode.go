package sqlite

import (
	"fmt"
	"math"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Value encoding between domain types and columns (see 0001_init.sql).

// gaugeScale is the fixed-point factor for gauges: two decimals.
const gaugeScale = 100

func encGauge(v float64) int64 { return int64(math.Round(v * gaugeScale)) }
func decGauge(i int64) float64 { return float64(i) / gaugeScale }

func encTime(t time.Time) int64  { return t.UnixMilli() }
func decTime(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

func encID(id domain.ID) []byte {
	b := make([]byte, len(id))
	copy(b, id[:])
	return b
}

func decID(b []byte) (domain.ID, error) {
	var id domain.ID
	if len(b) != len(id) {
		return id, fmt.Errorf("stored id has %d bytes, want %d", len(b), len(id))
	}
	copy(id[:], b)
	return id, nil
}

// decU64 reads a value written by encU64. A negative value cannot have been
// written by this adapter: it means corruption, and fails loudly.
func decU64(v int64) (uint64, error) {
	if v < 0 {
		return 0, fmt.Errorf("stored unsigned value is negative (%d): database corrupt", v)
	}
	return uint64(v), nil
}

// decInto decodes stored unsigned values into their destinations, in order.
func decInto(vals []int64, dst []*uint64) error {
	for i, v := range vals {
		u, err := decU64(v)
		if err != nil {
			return err
		}
		*dst[i] = u
	}
	return nil
}

// encU64 stores an unsigned value in a signed INTEGER column. domain
// validation bounds every value to MaxCounter, so this never wraps; the check
// makes a bypassed validation fail loudly instead of storing a negative.
func encU64(v uint64) (int64, error) {
	if v > domain.MaxCounter {
		return 0, fmt.Errorf("value %d does not fit a signed 64-bit column", v)
	}
	return int64(v), nil
}
