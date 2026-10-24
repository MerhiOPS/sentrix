// Package wire defines the agent↔server ingest protocol (/ingest/v1). It is
// shared by the agent and infra/ingesthttp only, imports the standard
// library only, and changes additively: a new field must be optional.
package wire

// Paths of the ingest endpoints.
const (
	PathEnroll = "/ingest/v1/enroll"
	PathBatch  = "/ingest/v1/batch"
)

// Batch is one POST /ingest/v1/batch body (gzip-compressed JSON).
type Batch struct {
	// BatchSeq is strictly increasing per host and never repeats, even if the
	// agent loses its state: the agent floors it by epoch nanoseconds. The
	// server acks a seq it has already stored as a replay without re-applying.
	BatchSeq   uint64     `json:"batch_seq"`
	IntervalMS int64      `json:"interval_ms"`
	Samples    []Sample   `json:"samples"`
	Details    []Detail   `json:"details,omitempty"`   // Phase 3; absent from older agents
	Processes  []Snapshot `json:"processes,omitempty"` // Phase 3
	Agent      AgentStats `json:"agent"`
}

// Detail is one value of a per-core / per-mount / per-device / per-NIC
// series. Counters are raw, like core counters.
type Detail struct {
	TSMS   int64             `json:"ts_ms"`
	Metric string            `json:"metric"`
	Kind   string            `json:"kind"`
	Labels map[string]string `json:"labels"`
	Value  float64           `json:"value"`
}

// Snapshot is a top-N process list. Names only — never command lines.
type Snapshot struct {
	TSMS  int64     `json:"ts_ms"`
	Procs []Process `json:"procs"`
}

// Process is one snapshot entry.
type Process struct {
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"`
	RSS  uint64  `json:"rss"`
}

// Sample is one CoreSample on the wire. Gauges are float64; counters are the
// raw monotonic totals read on the host — the server computes rates.
type Sample struct {
	TSMS           int64   `json:"ts_ms"`
	CPUPercent     float64 `json:"cpu"`
	MemoryPercent  float64 `json:"mem"`
	DiskPercent    float64 `json:"disk"`
	Load1          float64 `json:"load1"`
	Load5          float64 `json:"load5"`
	Load15         float64 `json:"load15"`
	MemoryUsed     uint64  `json:"mem_used"`
	UptimeSeconds  uint64  `json:"uptime"`
	NetRxBytes     uint64  `json:"net_rx"`
	NetTxBytes     uint64  `json:"net_tx"`
	DiskReadBytes  uint64  `json:"disk_r"`
	DiskWriteBytes uint64  `json:"disk_w"`
}

// AgentStats is the agent's report on itself, shown per host.
type AgentStats struct {
	Version      string `json:"version"`
	BufferDepth  int    `json:"buffer_depth"`  // batches waiting to be sent
	DroppedTotal uint64 `json:"dropped_total"` // batches dropped since start
	ClockSkewMS  int64  `json:"clock_skew_ms"` // agent clock minus server clock, last ack
}

// Ack is the 200 response to a batch. Every 200 means "do not resend":
// either the batch was stored, or it had been stored before (Replayed).
type Ack struct {
	BatchSeq   uint64 `json:"batch_seq"`
	ServerTSMS int64  `json:"server_ts_ms"`
	Replayed   bool   `json:"replayed"`
	// Rejected counts samples refused individually (timestamp outside the
	// accepted window, invalid values). They are counted, never retried.
	Rejected int `json:"rejected"`
	// LogsShed counts log lines refused (quota, disk) while metrics were
	// stored. The agent counts them and does not resend them (Phase 4).
	LogsShed       int    `json:"logs_shed"`
	LogsShedReason string `json:"logs_shed_reason,omitempty"`
}

// Error is the body of every non-200 ingest response.
type Error struct {
	Error string `json:"error"`
}
