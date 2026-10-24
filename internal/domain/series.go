package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Labels distinguish series of one metric on one host, e.g. {"core": "3"}.
type Labels map[string]string

// Label limits keep one misbehaving agent from exploding series storage.
const (
	MaxLabels        = 8
	MaxLabelKeyLen   = 64
	MaxLabelValueLen = 256
)

// Validate enforces the label limits and forbids empty keys.
func (l Labels) Validate() error {
	if len(l) > MaxLabels {
		return fmt.Errorf("%w: %d labels, max %d", ErrInvalid, len(l), MaxLabels)
	}
	for k, v := range l {
		if k == "" || len(k) > MaxLabelKeyLen {
			return fmt.Errorf("%w: label key %q", ErrInvalid, k)
		}
		if len(v) > MaxLabelValueLen {
			return fmt.Errorf("%w: label %q value too long", ErrInvalid, k)
		}
	}
	return nil
}

// Canonical renders labels as sorted key=value pairs, so that the same set
// always has one spelling regardless of map order or how the JSON was
// formatted (PLAN Phase 3: {"core":"3"} and {"core": "3"} are one series).
// Keys and values are quoted, so "=" or "," inside them cannot collide.
func (l Labels) Canonical() string {
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%q=%q", k, l[k])
	}
	return b.String()
}

// SeriesKey identifies a series within a host: metric name plus the hash of
// its canonical labels.
type SeriesKey struct {
	Metric     string
	LabelsHash string
}

// KeyFor builds the series key for a metric and its labels.
func KeyFor(metric string, l Labels) SeriesKey {
	sum := sha256.Sum256([]byte(metric + "\x00" + l.Canonical()))
	return SeriesKey{Metric: metric, LabelsHash: hex.EncodeToString(sum[:16])}
}

// Series is a resolved variable-cardinality series.
type Series struct {
	ID     int64
	Host   HostID
	Key    SeriesKey
	Kind   MetricKind
	Labels Labels
}
