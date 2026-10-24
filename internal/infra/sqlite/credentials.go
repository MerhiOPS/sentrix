package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/ingest"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// AuthenticateCredential implements ingest.CredentialStore. It is one of the
// two deliberately unscoped reads (with RedeemToken): the credential hash is
// what identifies the tenant.
func (s *Store) AuthenticateCredential(ctx context.Context, credHash []byte) (ingest.AuthHost, error) {
	var tenantRaw, hostRaw []byte
	var intervalMS int64
	var disabled sql.NullInt64
	err := s.db.Reader().QueryRowContext(ctx, `SELECT t.id, h.id, h.interval_ms, h.disabled_at
		FROM hosts h JOIN tenants t ON t.key = h.tenant_key
		WHERE h.credential_hash = ?`, credHash).Scan(&tenantRaw, &hostRaw, &intervalMS, &disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return ingest.AuthHost{}, fmt.Errorf("credential: %w", domain.ErrNotFound)
	}
	if err != nil {
		return ingest.AuthHost{}, fmt.Errorf("authenticate credential: %w", err)
	}
	tid, err := decID(tenantRaw)
	if err != nil {
		return ingest.AuthHost{}, err
	}
	hid, err := decID(hostRaw)
	if err != nil {
		return ingest.AuthHost{}, err
	}
	return ingest.AuthHost{
		Tenant: domain.TenantID(tid), Host: domain.HostID(hid),
		Interval: time.Duration(intervalMS) * time.Millisecond, Enabled: !disabled.Valid,
	}, nil
}
