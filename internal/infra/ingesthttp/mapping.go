package ingesthttp

import (
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/wire"
)

// toBatch maps the wire batch onto the use case's input. Identity fields do
// not exist on the wire batch at all: tenant and host come from the
// credential, so a forged host_id in a body is not even parsed.
func toBatch(b wire.Batch) ingest.Batch {
	out := ingest.Batch{
		Seq:      b.BatchSeq,
		Interval: time.Duration(b.IntervalMS) * time.Millisecond,
		Samples:  make([]domain.CoreSample, 0, len(b.Samples)),
		Agent: ingest.AgentStats{
			Version: b.Agent.Version, BufferDepth: b.Agent.BufferDepth,
			DroppedTotal: b.Agent.DroppedTotal, ClockSkew: time.Duration(b.Agent.ClockSkewMS) * time.Millisecond,
		},
	}
	for _, s := range b.Samples {
		out.Samples = append(out.Samples, domain.CoreSample{
			TS:         time.UnixMilli(s.TSMS).UTC(),
			CPUPercent: s.CPUPercent, MemoryPercent: s.MemoryPercent, DiskPercent: s.DiskPercent,
			Load1: s.Load1, Load5: s.Load5, Load15: s.Load15,
			MemoryUsed: s.MemoryUsed, UptimeSeconds: s.UptimeSeconds,
			NetRxBytes: s.NetRxBytes, NetTxBytes: s.NetTxBytes,
			DiskReadBytes: s.DiskReadBytes, DiskWriteBytes: s.DiskWriteBytes,
		})
	}
	return out
}

func toAck(seq uint64, r ingest.Result) wire.Ack {
	return wire.Ack{
		BatchSeq: seq, ServerTSMS: r.ServerNow.UnixMilli(),
		Replayed: r.Replayed, Rejected: r.Rejected,
	}
}

func toHostInfo(h wire.HostInfo) identity.HostInfo {
	return identity.HostInfo{
		Hostname: h.Hostname, OS: h.OS, Platform: h.Platform,
		AgentVersion: h.AgentVersion, Interval: time.Duration(h.IntervalMS) * time.Millisecond,
	}
}
