package query

// IsKnownMetric reports whether name is a chartable core metric — the set
// alert rules may watch.
func IsKnownMetric(name string) bool { return isKnown(Gauge(name)) }

// IsRate reports whether the metric is a counter's rate.
func IsRate(name string) bool {
	_, ok := rateCounter[Gauge(name)]
	return ok
}

// BucketValue reduces one rollup bucket to the metric's value, the same way
// a chart at 1m resolution does: gauges by agg ("avg", "max" or "min"), a
// rate as the counter's growth over the span its samples cover. A rate has
// one value per bucket, so agg does not apply to it. ok is false when the
// bucket has nothing to say (no samples; a rate from a single sample).
func BucketValue(b RollupBucket, metric, agg string) (float64, bool) {
	g := Gauge(metric)
	if c, isRate := rateCounter[g]; isRate {
		span := b.LastTS.Sub(b.FirstTS).Seconds()
		e, ok := b.Counters[c]
		if !ok || span <= 0 {
			return 0, false
		}
		return (e.Last - e.First) / span, true
	}
	a, ok := b.Gauges[g]
	if !ok || b.N <= 0 {
		return 0, false
	}
	switch agg {
	case "max":
		return a.Max, true
	case "min":
		return a.Min, true
	default:
		return a.Sum / float64(b.N), true
	}
}
