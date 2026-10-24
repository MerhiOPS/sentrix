package query

import (
	"fmt"
	"math"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// TargetPoints is what an automatic step aims for: enough for a chart's
// width, far below MaxPoints.
const TargetPoints = 300

// Source is where a frame's data came from.
type Source string

const (
	SourceRaw Source = "raw"
	Source1m  Source = "1m"
	Source1h  Source = "1h"
)

// Rate metrics: per-second rates of the core counters.
const (
	RateNetRx Gauge = "net_rx_rate"
	RateNetTx Gauge = "net_tx_rate"
	RateDiskR Gauge = "disk_r_rate"
	RateDiskW Gauge = "disk_w_rate"
)

// rateCounter maps a rate metric to its counter.
var rateCounter = map[Gauge]string{
	RateNetRx: CounterNetRx, RateNetTx: CounterNetTx, RateDiskR: CounterDiskR, RateDiskW: CounterDiskW,
}

func isKnown(g Gauge) bool {
	_, gauge := gaugeValue[g]
	_, rate := rateCounter[g]
	return gauge || rate
}

// plan decides step and source. An explicit step is honoured (and capped);
// an automatic one targets TargetPoints. The source is the coarsest store
// whose resolution still fits inside one step, so a 30-day chart never
// touches raw rows.
func plan(req SeriesRequest, interval time.Duration) (time.Duration, Source, error) {
	step := req.Step
	if step == 0 {
		auto := time.Duration(math.Ceil(float64(req.To.Sub(req.From)) / TargetPoints))
		step = max(interval, auto.Round(time.Second))
	}
	if n := int(math.Ceil(float64(req.To.Sub(req.From)) / float64(step))); n > MaxPoints {
		return 0, "", fmt.Errorf("%w: %d points requested, max %d — use a coarser step", domain.ErrInvalid, n, MaxPoints)
	}
	switch {
	case step >= time.Hour:
		return step, Source1h, nil
	case step >= time.Minute:
		return step, Source1m, nil
	default:
		return step, SourceRaw, nil
	}
}
