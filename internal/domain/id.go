// Package domain holds Sentrix's entities and value objects. It imports the
// standard library only; storage, transport and OS concerns live in infra.
package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// ID is a random 128-bit identifier rendered as a canonical UUID string
// (version 4, RFC 9562 variant). Typed wrappers below keep a HostID from
// being passed where a TenantID is expected.
type ID [16]byte

// NewID returns a random version-4 ID. crypto/rand never fails on supported
// platforms (Go 1.24+ panics internally instead), so there is no error.
func NewID() ID {
	var id ID
	_, _ = rand.Read(id[:])
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return id
}

// ParseID accepts the canonical 36-character hyphenated form only, so every
// stored or logged ID has one spelling.
func ParseID(s string) (ID, error) {
	var id ID
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return id, fmt.Errorf("%w: id %q is not a canonical UUID", ErrInvalid, s)
	}
	hexPart := s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:36]
	if _, err := hex.Decode(id[:], []byte(hexPart)); err != nil {
		return ID{}, fmt.Errorf("%w: id %q: %w", ErrInvalid, s, err)
	}
	return id, nil
}

// String renders the canonical lower-case hyphenated form.
func (id ID) String() string {
	var b [36]byte
	hex.Encode(b[0:8], id[0:4])
	b[8] = '-'
	hex.Encode(b[9:13], id[4:6])
	b[13] = '-'
	hex.Encode(b[14:18], id[6:8])
	b[18] = '-'
	hex.Encode(b[19:23], id[8:10])
	b[23] = '-'
	hex.Encode(b[24:36], id[10:16])
	return string(b[:])
}

// IsZero reports whether the ID was never assigned.
func (id ID) IsZero() bool { return id == ID{} }

// TenantID identifies a tenant: the unit of isolation.
type TenantID ID

// HostID identifies a monitored host within a tenant.
type HostID ID

// UserID identifies a dashboard user within a tenant.
type UserID ID

func (t TenantID) String() string { return ID(t).String() }
func (t TenantID) IsZero() bool   { return ID(t).IsZero() }
func (h HostID) String() string   { return ID(h).String() }
func (h HostID) IsZero() bool     { return ID(h).IsZero() }
func (u UserID) String() string   { return ID(u).String() }
func (u UserID) IsZero() bool     { return ID(u).IsZero() }

// NewTenantID, NewHostID and NewUserID mint fresh typed IDs.
func NewTenantID() TenantID { return TenantID(NewID()) }
func NewHostID() HostID     { return HostID(NewID()) }
func NewUserID() UserID     { return UserID(NewID()) }

// ParseTenantID parses a canonical tenant ID.
func ParseTenantID(s string) (TenantID, error) {
	id, err := ParseID(s)
	return TenantID(id), err
}

// ParseHostID parses a canonical host ID.
func ParseHostID(s string) (HostID, error) {
	id, err := ParseID(s)
	return HostID(id), err
}

// ParseUserID parses a canonical user ID.
func ParseUserID(s string) (UserID, error) {
	id, err := ParseID(s)
	return UserID(id), err
}
