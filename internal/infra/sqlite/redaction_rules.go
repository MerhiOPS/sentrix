package sqlite

import (
	"context"
	"fmt"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// RedactionRules implements redaction.RuleRepo and redact.RuleSource.
func (s *Store) RedactionRules(ctx context.Context, sc domain.Scope) ([]domain.RedactionRule, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	rows, err := r.QueryContext(ctx, `SELECT id, pattern, replacement, action FROM redaction_rules
		WHERE tenant_key = ? ORDER BY id`, tk)
	if err != nil {
		return nil, fmt.Errorf("list redaction rules: %w", err)
	}
	defer rows.Close()
	var out []domain.RedactionRule
	for rows.Next() {
		var rr domain.RedactionRule
		var action string
		if err := rows.Scan(&rr.ID, &rr.Pattern, &rr.Replacement, &action); err != nil {
			return nil, fmt.Errorf("scan redaction rule: %w", err)
		}
		rr.Action = domain.RedactionAction(action)
		out = append(out, rr)
	}
	return out, rows.Err()
}

// CreateRedactionRule implements redaction.RuleRepo.
func (s *Store) CreateRedactionRule(ctx context.Context, sc domain.Scope, rr domain.RedactionRule) (int64, error) {
	w := s.db.Writer()
	tk, err := tenantKey(ctx, w, sc)
	if err != nil {
		return 0, err
	}
	res, err := w.ExecContext(ctx, `INSERT INTO redaction_rules (tenant_key, pattern, replacement, action, created_at)
		VALUES (?, ?, ?, ?, ?)`, tk, rr.Pattern, rr.Replacement, string(rr.Action), encTime(s.now()))
	if err != nil {
		return 0, fmt.Errorf("insert redaction rule: %w", err)
	}
	return res.LastInsertId()
}

// DeleteRedactionRule implements redaction.RuleRepo.
func (s *Store) DeleteRedactionRule(ctx context.Context, sc domain.Scope, id int64) error {
	w := s.db.Writer()
	tk, err := tenantKey(ctx, w, sc)
	if err != nil {
		return err
	}
	res, err := w.ExecContext(ctx, `DELETE FROM redaction_rules WHERE tenant_key = ? AND id = ?`, tk, id)
	if err != nil {
		return fmt.Errorf("delete redaction rule: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("redaction rule: %w", domain.ErrNotFound)
	}
	return nil
}
