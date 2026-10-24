package ingest

import "github.com/MerhiOPS/sentrix/internal/domain"

// Counter names a monotonic counter in CoreSample.
type Counter string

const (
	NetRx     Counter = "net_rx"
	NetTx     Counter = "net_tx"
	DiskRead  Counter = "disk_read"
	DiskWrite Counter = "disk_write"
)

// Counters lists every core counter, in a fixed order.
var Counters = []Counter{NetRx, NetTx, DiskRead, DiskWrite}

// CounterValue is the persisted state of one counter on one host.
type CounterValue struct {
	LastRaw uint64 // last raw value the agent sent
	Offset  uint64 // added to raw values to make the stored series monotonic
}

// CounterState is a host's counter state, keyed by counter.
type CounterState map[Counter]CounterValue

// clone returns an independent copy, so folding works on a draft (R1).
func (s CounterState) clone() CounterState {
	out := make(CounterState, len(s))
	for k, v := range s {
		out[k] = v
	}
	return out
}

func field(s *domain.CoreSample, c Counter) *uint64 {
	switch c {
	case NetRx:
		return &s.NetRxBytes
	case NetTx:
		return &s.NetTxBytes
	case DiskRead:
		return &s.DiskReadBytes
	default:
		return &s.DiskWriteBytes
	}
}

// foldCounters rewrites raw counters as reset-adjusted, monotonic values. A
// raw value below the previous one means the counter restarted (reboot,
// driver reload, 32-bit wrap): the offset absorbs the previous raw value, so
// the stored series continues from where it was instead of dropping — and a
// rate computed from it is never negative.
//
// Samples must be in arrival order. state is not modified; the new state is
// returned, to be committed with the rows it produced.
func foldCounters(state CounterState, samples []domain.CoreSample) ([]domain.CoreSample, CounterState) {
	next := state.clone()
	out := make([]domain.CoreSample, len(samples))
	copy(out, samples)
	for i := range out {
		for _, c := range Counters {
			f := field(&out[i], c)
			cv, seen := next[c]
			if seen && *f < cv.LastRaw {
				cv.Offset += cv.LastRaw
			}
			cv.LastRaw = *f
			next[c] = cv
			*f += cv.Offset
		}
	}
	return out, next
}
