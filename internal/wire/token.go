package wire

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

// tokenPrefix versions the enrollment token format.
const tokenPrefix = "stx1"

// SecretLen is the length of enrollment secrets and credentials in bytes.
const SecretLen = 32

// PinLen is the length of an SPKI pin: SHA-256 of the server's public key.
const PinLen = 32

// ErrBadToken means the string is not a well-formed enrollment token.
var ErrBadToken = errors.New("malformed enrollment token")

// Token is a parsed enrollment token. Pin is nil when the server uses a
// CA-issued certificate: the agent then verifies with system roots instead,
// because pinning a CA leaf would brick every agent at renewal (PLAN §2).
type Token struct {
	Secret []byte
	Pin    []byte
}

// String renders "stx1.<secret>[.<pin>]": secret as unpadded base64url, pin
// as hex so an operator can compare it with the server's printed pin.
func (t Token) String() string {
	s := tokenPrefix + "." + base64.RawURLEncoding.EncodeToString(t.Secret)
	if t.Pin != nil {
		s += "." + hex.EncodeToString(t.Pin)
	}
	return s
}

// ParseToken parses and length-checks a token.
func ParseToken(s string) (Token, error) {
	parts := strings.Split(strings.TrimSpace(s), ".")
	if len(parts) < 2 || len(parts) > 3 || parts[0] != tokenPrefix {
		return Token{}, ErrBadToken
	}
	secret, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(secret) != SecretLen {
		return Token{}, ErrBadToken
	}
	t := Token{Secret: secret}
	if len(parts) == 3 {
		pin, err := hex.DecodeString(parts[2])
		if err != nil || len(pin) != PinLen {
			return Token{}, ErrBadToken
		}
		t.Pin = pin
	}
	return t, nil
}

// EncodeSecret renders a credential or token secret for transport.
func EncodeSecret(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// DecodeSecret parses a transported credential, rejecting wrong lengths.
func DecodeSecret(s string) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) != SecretLen {
		return nil, errors.New("malformed credential")
	}
	return b, nil
}
