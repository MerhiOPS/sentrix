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

	// MaxEnrollBytes caps the enrollment request.
	MaxEnrollBytes = 16 << 10
)
