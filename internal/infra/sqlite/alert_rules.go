package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

const ruleColumns = `key, name, kind, metric, aggregation, comparator, threshold, duration_ms, on_no_data, cooldown_ms, enabled`

// ListRules implements alerting.RuleRepo, ordered by id.
func (s *Store) ListRules(ctx context.Context, sc domain.Scope) ([]domain.AlertRule, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	rows, err := r.QueryContext(ctx, `SELECT `+ruleColumns+` FROM alert_rules WHERE tenant_key = ? ORDER BY key`, tk)
	if err != nil {
		return nil, fmt.Errorf("list rules: %w", err)
	}
	defer rows.Close()
	var out []domain.AlertRule
	index := map[int64]int{}
	for rows.Next() {
		rule, err := scanRule(rows.Scan)
		if err != nil {
			return nil, err
		}
		index[rule.ID] = len(out)
		out = append(out, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.ruleLinks(ctx, r, tk, out, index)
}

func scanRule(scan func(...any) error) (domain.AlertRule, error) {
	var r domain.AlertRule
	var kind, agg, cmp, noData string
	var dur, cool, enabled int64
	if err := scan(&r.ID, &r.Name, &kind, &r.Metric, &agg, &cmp, &r.Threshold, &dur, &noData, &cool, &enabled); err != nil {
		return r, fmt.Errorf("scan rule: %w", err)
	}
	r.Kind, r.Aggregation, r.Comparator, r.OnNoData = domain.RuleKind(kind), domain.Aggregation(agg), domain.Comparator(cmp), domain.NoDataPolicy(noData)
	r.Duration, r.Cooldown, r.Enabled = time.Duration(dur)*time.Millisecond, time.Duration(cool)*time.Millisecond, enabled == 1
	return r, nil
}

// ruleLinks fills each rule's hosts and channels.
func (s *Store) ruleLinks(ctx context.Context, r *sql.DB, tk int64, rules []domain.AlertRule, index map[int64]int) error {
	rows, err := r.QueryContext(ctx, `SELECT l.rule_key, h.id FROM alert_rule_hosts l
		JOIN hosts h ON h.tenant_key = l.tenant_key AND h.key = l.host_key
		WHERE l.tenant_key = ? ORDER BY l.rule_key, h.name`, tk)
	if err != nil {
		return fmt.Errorf("read rule hosts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rule int64
		var raw []byte
		if err := rows.Scan(&rule, &raw); err != nil {
			return fmt.Errorf("scan rule host: %w", err)
		}
		id, err := decID(raw)
		if err != nil {
			return err
		}
		if i, ok := index[rule]; ok {
			rules[i].Hosts = append(rules[i].Hosts, domain.HostID(id))
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	crows, err := r.QueryContext(ctx, `SELECT rule_key, channel_key FROM alert_rule_channels
		WHERE tenant_key = ? ORDER BY rule_key, channel_key`, tk)
	if err != nil {
		return fmt.Errorf("read rule channels: %w", err)
	}
	defer crows.Close()
	for crows.Next() {
		var rule, ch int64
		if err := crows.Scan(&rule, &ch); err != nil {
			return fmt.Errorf("scan rule channel: %w", err)
		}
		if i, ok := index[rule]; ok {
			rules[i].Channels = append(rules[i].Channels, ch)
		}
	}
	return crows.Err()
}

// CreateRule implements alerting.RuleRepo.
func (s *Store) CreateRule(ctx context.Context, sc domain.Scope, r domain.AlertRule) (int64, error) {
	var id int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		tk, err := tenantKey(ctx, tx, sc)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO alert_rules (tenant_key, name, kind, metric, aggregation, comparator,
			threshold, duration_ms, on_no_data, cooldown_ms, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			append([]any{tk}, append(ruleValues(r), encTime(s.now()))...)...)
		if err != nil {
			return fmt.Errorf("insert rule: %w", err)
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		return linkRule(ctx, tx, tk, id, r)
	})
	return id, err
}

// UpdateRule implements alerting.RuleRepo. Links are replaced; state and
// history are kept.
func (s *Store) UpdateRule(ctx context.Context, sc domain.Scope, r domain.AlertRule) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		tk, err := tenantKey(ctx, tx, sc)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE alert_rules SET name = ?, kind = ?, metric = ?, aggregation = ?,
			comparator = ?, threshold = ?, duration_ms = ?, on_no_data = ?, cooldown_ms = ?, enabled = ?
			WHERE tenant_key = ? AND key = ?`, append(ruleValues(r), tk, r.ID)...)
		if err != nil {
			return fmt.Errorf("update rule: %w", err)
		}
		if err := mustAffect(res, "alert rule"); err != nil {
			return err
		}
		for _, q := range []string{`DELETE FROM alert_rule_hosts WHERE tenant_key = ? AND rule_key = ?`,
			`DELETE FROM alert_rule_channels WHERE tenant_key = ? AND rule_key = ?`} {
			if _, err := tx.ExecContext(ctx, q, tk, r.ID); err != nil {
				return fmt.Errorf("unlink rule: %w", err)
			}
		}
		return linkRule(ctx, tx, tk, r.ID, r)
	})
}

// DeleteRule implements alerting.RuleRepo; links, state and events cascade.
func (s *Store) DeleteRule(ctx context.Context, sc domain.Scope, id int64) error {
	return s.deleteOwned(ctx, sc, `DELETE FROM alert_rules WHERE tenant_key = ? AND key = ?`, id, "alert rule")
}

func ruleValues(r domain.AlertRule) []any {
	enabled := 0
	if r.Enabled {
		enabled = 1
	}
	return []any{r.Name, string(r.Kind), r.Metric, string(r.Aggregation), string(r.Comparator), r.Threshold,
		r.Duration.Milliseconds(), string(r.OnNoData), r.Cooldown.Milliseconds(), enabled}
}

// linkRule stores a rule's hosts and channels. Each is resolved inside the
// tenant first: another tenant's host or channel is not found.
func linkRule(ctx context.Context, tx *sql.Tx, tk, rule int64, r domain.AlertRule) error {
	for _, h := range r.Hosts {
		hk, err := hostKey(ctx, tx, tk, h)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO alert_rule_hosts (tenant_key, rule_key, host_key)
			VALUES (?, ?, ?)`, tk, rule, hk); err != nil {
			return fmt.Errorf("link rule host: %w", err)
		}
	}
	for _, c := range r.Channels {
		if err := channelExists(ctx, tx, tk, c); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO alert_rule_channels (tenant_key, rule_key, channel_key)
			VALUES (?, ?, ?)`, tk, rule, c); err != nil {
			return fmt.Errorf("link rule channel: %w", err)
		}
	}
	return nil
}

func channelExists(ctx context.Context, q rowQuerier, tk, id int64) error {
	var one int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM channels WHERE tenant_key = ? AND key = ?`, tk, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("channel: %w", domain.ErrNotFound)
	}
	return err
}

// inTx runs fn in one writer transaction.
func (s *Store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// mustAffect turns "no row matched" into not found.
func mustAffect(res sql.Result, what string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%s: %w", what, domain.ErrNotFound)
	}
	return nil
}
