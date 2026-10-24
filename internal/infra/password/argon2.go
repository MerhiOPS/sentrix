// Package password implements identity.PasswordHasher with Argon2id.
package password

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Parameters are pinned, not tuned at runtime. RFC 9106's 64 MiB
// recommendation is a trivial DoS on a small server: four concurrent logins
// on two cores would need 256 MiB and pin both cores. 46 MiB with t=1, p=1 is
// OWASP's second Argon2id option and, with the semaphore, bounds login memory
// to Concurrency × 46 MiB.
const (
	memoryKiB   = 46 * 1024
	iterations  = 1
	parallelism = 1
	saltLen     = 16
	keyLen      = 32
)

// Argon2id hashes passwords, at most Concurrency at a time.
type Argon2id struct {
	sem chan struct{}
}

// New allows up to concurrency simultaneous hash operations.
func New(concurrency int) *Argon2id {
	return &Argon2id{sem: make(chan struct{}, max(concurrency, 1))}
}

func (a *Argon2id) acquire(ctx context.Context) error {
	select {
	case a.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *Argon2id) release() { <-a.sem }

// Hash returns "$argon2id$v=19$m=…,t=…,p=…$salt$key" (PHC string format).
func (a *Argon2id) Hash(ctx context.Context, password string) (string, error) {
	if err := a.acquire(ctx); err != nil {
		return "", err
	}
	defer a.release()
	salt := make([]byte, saltLen)
	_, _ = rand.Read(salt)
	key := argon2.IDKey([]byte(password), salt, iterations, memoryKiB, parallelism, keyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memoryKiB, iterations, parallelism, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify checks a password against an encoded hash, using the parameters
// stored in the hash (so old hashes keep verifying after a parameter change).
func (a *Argon2id) Verify(ctx context.Context, password, encoded string) (bool, error) {
	p, err := parse(encoded)
	if err != nil {
		return false, err
	}
	if err := a.acquire(ctx); err != nil {
		return false, err
	}
	defer a.release()
	got := argon2.IDKey([]byte(password), p.salt, p.t, p.m, p.p, uint32(len(p.key))) //nolint:gosec // key length bounded by parse
	return subtle.ConstantTimeCompare(got, p.key) == 1, nil
}

type params struct {
	m, t      uint32
	p         uint8
	salt, key []byte
}

var errFormat = errors.New("password hash is not in argon2id PHC format")

// parse decodes and bounds a PHC string; out-of-range parameters are refused
// so a tampered row cannot make verification allocate gigabytes.
func parse(s string) (params, error) {
	parts := strings.Split(s, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != fmt.Sprintf("v=%d", argon2.Version) {
		return params{}, errFormat
	}
	var p params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.m, &p.t, &p.p); err != nil {
		return params{}, errFormat
	}
	if p.m < 8*1024 || p.m > 256*1024 || p.t < 1 || p.t > 10 || p.p < 1 || p.p > 8 {
		return params{}, fmt.Errorf("%w: parameters out of range", errFormat)
	}
	var err error
	b64 := base64.RawStdEncoding
	if p.salt, err = b64.DecodeString(parts[4]); err != nil || len(p.salt) < 8 {
		return params{}, errFormat
	}
	if p.key, err = b64.DecodeString(parts[5]); err != nil || len(p.key) < 16 || len(p.key) > 64 {
		return params{}, errFormat
	}
	return p, nil
}
