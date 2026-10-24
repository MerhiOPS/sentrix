package domain

import (
	"fmt"
	"strings"
	"time"
)

// Tenant is the unit of isolation: every host, user and sample belongs to
// exactly one.
type Tenant struct {
	ID        TenantID
	Name      string
	CreatedAt time.Time
}

// MaxTenantNameLen bounds tenant names for display and storage.
const MaxTenantNameLen = 100

// NormalizeTenantName trims and validates a tenant name.
func NormalizeTenantName(name string) (string, error) {
	n := strings.TrimSpace(name)
	if n == "" || len(n) > MaxTenantNameLen {
		return "", fmt.Errorf("%w: tenant name must be 1-%d characters", ErrInvalid, MaxTenantNameLen)
	}
	return n, nil
}
