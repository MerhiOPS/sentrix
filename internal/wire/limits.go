package wire

// Ingest limits, enforced by the server and respected by the agent. A batch
// over a limit is permanently invalid (413/422): the agent drops and counts
// it, never retries it (PLAN §2).
const (
	// MaxCompressedBytes caps the request body as sent.
	MaxCompressedBytes = 1 << 20 // 1 MiB

	// MaxDecompressedBytes caps the body after gunzip, separately: a small
	// compressed body can inflate a thousandfold (gzip bomb).
	MaxDecompressedBytes = 8 << 20 // 8 MiB

	// MaxSamplesPerBatch caps core samples per batch (OLD A-M5). At a 5 s
	// interval this is over an hour of backlog in one request.
	MaxSamplesPerBatch = 1000

	// MaxDetailsPerBatch caps detail values per batch: ~30 series × 2
	// samples is normal; this leaves room for a big host's backlog.
	MaxDetailsPerBatch = 20000

	// MaxSnapshotsPerBatch caps process snapshots per batch.
	MaxSnapshotsPerBatch = 200

	// MaxLogsPerBatch caps log lines per batch. Message size is capped by
	// the domain (16 KiB); MaxDecompressedBytes bounds the total.
	MaxLogsPerBatch = 5000

	// MaxEnrollBytes caps the enrollment request.
	MaxEnrollBytes = 16 << 10
)
