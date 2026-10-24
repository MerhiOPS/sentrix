package domain

import "errors"

// Sentinel errors shared by every layer. Adapters wrap them with context;
// callers test with errors.Is.
var (
	// ErrNotFound: the entity does not exist *in this scope*. Another tenant's
	// entity is also "not found" — existence is never leaked across tenants.
	ErrNotFound = errors.New("not found")

	// ErrInvalid: input violates a domain rule (bad ID, non-finite value, …).
	ErrInvalid = errors.New("invalid")

	// ErrConflict: the write collides with existing state (duplicate name,
	// token already used, …).
	ErrConflict = errors.New("conflict")

	// ErrQuotaExceeded: a plan limit refuses the operation.
	ErrQuotaExceeded = errors.New("quota exceeded")

	// ErrUnauthenticated: no valid credential or session.
	ErrUnauthenticated = errors.New("unauthenticated")

	// ErrForbidden: authenticated, but the role may not do this.
	ErrForbidden = errors.New("forbidden")
)
