package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/app/identity"
	"github.com/MerhiOPS/sentrix/internal/domain"
)

// CreateToken implements identity.EnrollmentRepo.
func (s *Store) CreateToken(ctx context.Context, sc domain.Scope, t identity.EnrollmentToken) error {
	w := s.db.Writer()
	tk, err := tenantKey(ctx, w, sc)
	if err != nil {
		return err
	}
	_, err = w.ExecContext(ctx,
		`INSERT INTO enrollment_tokens (secret_hash, tenant_key, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		t.SecretHash, tk, encTime(t.CreatedAt), encTime(t.ExpiresAt))
	if err != nil {
		return fmt.Errorf("insert token: %w", err)
	}
	return nil
}

// RedeemToken implements identity.EnrollmentRepo: consume the token and
// create the host in one transaction. The consuming UPDATE is the
// single-use guarantee — two agents racing with one token cannot both see
// one row affected.
func (s *Store) RedeemToken(ctx context.Context, secretHash []byte, now time.Time, nh identity.NewHost) (domain.Host, error) {
	tx, err := s.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return domain.Host{}, fmt.Errorf("begin redeem: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	tk, err := consumeToken(ctx, tx, secretHash, now)
	if err != nil {
		return domain.Host{}, err
	}
	// Over max_hosts only new enrollments are refused; the rollback leaves
	// the token unused, so it works once a slot frees up.
	if full, err := hostsAtLimit(ctx, tx, tk); err != nil || full {
		if err == nil {
			err = fmt.Errorf("%w: the plan's host limit is reached", domain.ErrQuotaExceeded)
		}
		return domain.Host{}, err
	}
	h := nh.Host
	res, err := tx.ExecContext(ctx, `INSERT INTO hosts
		(tenant_key, id, name, os, platform, agent_version, interval_ms, created_at, credential_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tk, encID(domain.ID(h.ID)), h.Name, h.OS, h.Platform, h.AgentVersion,
		h.Interval.Milliseconds(), encTime(h.CreatedAt), nh.CredentialHash)
	if err != nil {
		return domain.Host{}, fmt.Errorf("insert host: %w", err)
	}
	hk, err := res.LastInsertId()
	if err != nil {
		return domain.Host{}, fmt.Errorf("host key: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE enrollment_tokens SET host_key = ? WHERE secret_hash = ? AND tenant_key = ?`,
		hk, secretHash, tk); err != nil {
		return domain.Host{}, fmt.Errorf("link token: %w", err)
	}
	tid, err := tenantIDByKey(ctx, tx, tk)
	if err != nil {
		return domain.Host{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Host{}, fmt.Errorf("commit redeem: %w", err)
	}
	h.Tenant = tid
	return h, nil
}

// consumeToken marks an unused, unexpired token used and returns its tenant.
// Unknown, expired and used tokens are all ErrNotFound.
func consumeToken(ctx context.Context, tx *sql.Tx, secretHash []byte, now time.Time) (int64, error) {
	var tk int64
	err := tx.QueryRowContext(ctx, `UPDATE enrollment_tokens SET used_at = ?
		WHERE secret_hash = ? AND used_at IS NULL AND expires_at > ?
		RETURNING tenant_key`, encTime(now), secretHash, encTime(now)).Scan(&tk)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("enrollment token: %w", domain.ErrNotFound)
	}
	if err != nil {
		return 0, fmt.Errorf("consume token: %w", err)
	}
	return tk, nil
}

func tenantIDByKey(ctx context.Context, q rowQuerier, tk int64) (domain.TenantID, error) {
	var raw []byte
	if err := q.QueryRowContext(ctx, `SELECT id FROM tenants WHERE key = ?`, tk).Scan(&raw); err != nil {
		return domain.TenantID{}, fmt.Errorf("tenant by key: %w", err)
	}
	id, err := decID(raw)
	return domain.TenantID(id), err
}
