package identity

import (
	"crypto/rand"
	"crypto/sha256"
)

// SecretLen is the length of enrollment secrets and credentials. 32 random
// bytes need no slow hash: brute force is infeasible, and Argon2 per batch
// would make every ingest request a CPU DoS (PLAN Phase 1).
const SecretLen = 32

// NewSecret returns SecretLen random bytes.
func NewSecret() []byte {
	b := make([]byte, SecretLen)
	_, _ = rand.Read(b) // never fails on supported platforms (Go 1.24+)
	return b
}

// HashSecret is how secrets are stored and looked up: SHA-256. The database
// never holds a usable token or credential.
func HashSecret(secret []byte) []byte {
	sum := sha256.Sum256(secret)
	return sum[:]
}
