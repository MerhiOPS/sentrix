package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// ListChannels implements alerting.ChannelRepo. URLs are returned in full:
// masking is the use case's job.
func (s *Store) ListChannels(ctx context.Context, sc domain.Scope) ([]domain.Channel, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	rows, err := r.QueryContext(ctx, `SELECT key, name, kind, url FROM channels WHERE tenant_key = ? ORDER BY key`, tk)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	defer rows.Close()
	var out []domain.Channel
	for rows.Next() {
		var c domain.Channel
		var kind string
		if err := rows.Scan(&c.ID, &c.Name, &kind, &c.URL); err != nil {
			return nil, fmt.Errorf("scan channel: %w", err)
		}
		c.Kind = domain.ChannelKind(kind)
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateChannel implements alerting.ChannelRepo.
func (s *Store) CreateChannel(ctx context.Context, sc domain.Scope, c domain.Channel) (int64, error) {
	w := s.db.Writer()
	tk, err := tenantKey(ctx, w, sc)
	if err != nil {
		return 0, err
	}
	res, err := w.ExecContext(ctx, `INSERT INTO channels (tenant_key, name, kind, url, created_at) VALUES (?, ?, ?, ?, ?)`,
		tk, c.Name, string(c.Kind), c.URL, encTime(s.now()))
	if err != nil {
		return 0, fmt.Errorf("insert channel: %w", err)
	}
	return res.LastInsertId()
}

// UpdateChannel implements alerting.ChannelRepo.
func (s *Store) UpdateChannel(ctx context.Context, sc domain.Scope, c domain.Channel) error {
	w := s.db.Writer()
	tk, err := tenantKey(ctx, w, sc)
	if err != nil {
		return err
	}
	res, err := w.ExecContext(ctx, `UPDATE channels SET name = ?, kind = ?, url = ? WHERE tenant_key = ? AND key = ?`,
		c.Name, string(c.Kind), c.URL, tk, c.ID)
	if err != nil {
		return fmt.Errorf("update channel: %w", err)
	}
	return mustAffect(res, "channel")
}

// DeleteChannel implements alerting.ChannelRepo; rule links cascade.
func (s *Store) DeleteChannel(ctx context.Context, sc domain.Scope, id int64) error {
	return s.deleteOwned(ctx, sc, `DELETE FROM channels WHERE tenant_key = ? AND key = ?`, id, "channel")
}

// ListSilences implements alerting.SilenceRepo.
func (s *Store) ListSilences(ctx context.Context, sc domain.Scope) ([]domain.Silence, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	rows, err := r.QueryContext(ctx, `SELECT s.key, s.rule_key, h.id, s.starts, s.ends, s.reason FROM silences s
		LEFT JOIN hosts h ON h.tenant_key = s.tenant_key AND h.key = s.host_key
		WHERE s.tenant_key = ? ORDER BY s.key`, tk)
	if err != nil {
		return nil, fmt.Errorf("list silences: %w", err)
	}
	defer rows.Close()
	var out []domain.Silence
	for rows.Next() {
		var sil domain.Silence
		var rule sql.NullInt64
		var host []byte
		var starts, ends int64
		if err := rows.Scan(&sil.ID, &rule, &host, &starts, &ends, &sil.Reason); err != nil {
			return nil, fmt.Errorf("scan silence: %w", err)
		}
		if rule.Valid {
			sil.Rule = &rule.Int64
		}
		if host != nil {
			id, err := decID(host)
			if err != nil {
				return nil, err
			}
			h := domain.HostID(id)
			sil.Host = &h
		}
		sil.Starts, sil.Ends = decTime(starts), decTime(ends)
		out = append(out, sil)
	}
	return out, rows.Err()
}

// CreateSilence implements alerting.SilenceRepo. Its rule and host are
// resolved inside the tenant: another tenant's are not found.
func (s *Store) CreateSilence(ctx context.Context, sc domain.Scope, sil domain.Silence) (int64, error) {
	var id int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		tk, err := tenantKey(ctx, tx, sc)
		if err != nil {
			return err
		}
		var rule, host any
		if sil.Rule != nil {
			if err := ruleExists(ctx, tx, tk, *sil.Rule); err != nil {
				return err
			}
			rule = *sil.Rule
		}
		if sil.Host != nil {
			if host, err = hostKey(ctx, tx, tk, *sil.Host); err != nil {
				return err
			}
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO silences (tenant_key, rule_key, host_key, starts, ends, reason)
			VALUES (?, ?, ?, ?, ?, ?)`, tk, rule, host, encTime(sil.Starts), encTime(sil.Ends), sil.Reason)
		if err != nil {
			return fmt.Errorf("insert silence: %w", err)
		}
		id, err = res.LastInsertId()
		return err
	})
	return id, err
}

// DeleteSilence implements alerting.SilenceRepo.
func (s *Store) DeleteSilence(ctx context.Context, sc domain.Scope, id int64) error {
	return s.deleteOwned(ctx, sc, `DELETE FROM silences WHERE tenant_key = ? AND key = ?`, id, "silence")
}

func (s *Store) deleteOwned(ctx context.Context, sc domain.Scope, stmt string, id int64, what string) error {
	w := s.db.Writer()
	tk, err := tenantKey(ctx, w, sc)
	if err != nil {
		return err
	}
	res, err := w.ExecContext(ctx, stmt, tk, id)
	if err != nil {
		return fmt.Errorf("delete %s: %w", what, err)
	}
	return mustAffect(res, what)
}

func ruleExists(ctx context.Context, q rowQuerier, tk, id int64) error {
	var one int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM alert_rules WHERE tenant_key = ? AND key = ?`, tk, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("alert rule: %w", domain.ErrNotFound)
	}
	return err
}
