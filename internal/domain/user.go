package domain

import (
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// Role is what a dashboard user may do.
type Role string

const (
	RoleAdmin  Role = "admin"  // everything, including enrollment and user management
	RoleViewer Role = "viewer" // read-only
)

// ParseRole validates a role name.
func ParseRole(s string) (Role, error) {
	switch r := Role(strings.ToLower(strings.TrimSpace(s))); r {
	case RoleAdmin, RoleViewer:
		return r, nil
	default:
		return "", fmt.Errorf("%w: role %q", ErrInvalid, s)
	}
}

// Can reports whether the role satisfies a required role.
func (r Role) Can(required Role) bool {
	return r == RoleAdmin || r == required
}

// User is a dashboard login, owned by one tenant. Email is unique across
// tenants: the login form does not know the tenant, the email finds it.
type User struct {
	ID         UserID
	Tenant     TenantID
	Email      string
	Role       Role
	CreatedAt  time.Time
	DisabledAt *time.Time
}

// MaxEmailLen bounds login names (RFC 5321 path limit).
const MaxEmailLen = 254

// NormalizeEmail lower-cases and validates a login email. Display names and
// angle brackets are refused: the login name is the bare address.
func NormalizeEmail(s string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(s))
	if e == "" || len(e) > MaxEmailLen {
		return "", fmt.Errorf("%w: email must be 1-%d characters", ErrInvalid, MaxEmailLen)
	}
	addr, err := mail.ParseAddress(e)
	if err != nil || addr.Address != e || addr.Name != "" {
		return "", fmt.Errorf("%w: %q is not a bare email address", ErrInvalid, s)
	}
	return e, nil
}

// Password length bounds. The upper bound stops a megabyte "password" from
// being fed to the hash; 128 is far above any passphrase.
const (
	MinPasswordLen = 12
	MaxPasswordLen = 128
)

// ValidatePassword enforces the length policy (NIST SP 800-63B: length, no
// composition rules).
func ValidatePassword(p string) error {
	if n := len([]rune(p)); n < MinPasswordLen || len(p) > MaxPasswordLen {
		return fmt.Errorf("%w: password must be %d-%d characters", ErrInvalid, MinPasswordLen, MaxPasswordLen)
	}
	return nil
}

// Session is a logged-in browser. Only the hash of its token is stored.
type Session struct {
	TokenHash []byte
	User      UserID
	Tenant    TenantID
	Role      Role
	CreatedAt time.Time
	LastSeen  time.Time
}

// Session lifetimes: idle sessions end after SessionIdle, every session ends
// after SessionAbsolute regardless of activity.
const (
	SessionIdle     = 12 * time.Hour
	SessionAbsolute = 7 * 24 * time.Hour
)

// Expired reports whether the session is past its idle or absolute limit.
func (s Session) Expired(now time.Time) bool {
	return now.Sub(s.LastSeen) >= SessionIdle || now.Sub(s.CreatedAt) >= SessionAbsolute
}
