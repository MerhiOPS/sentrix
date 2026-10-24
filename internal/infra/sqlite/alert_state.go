package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// AlertStates implements alerting.StateRepo.
func (s *Store) AlertStates(ctx context.Context, sc domain.Scope) ([]domain.RuleHostState, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	rows, err := r.QueryContext(ctx, `SELECT a.rule_key, h.id, a.state, a.since, a.last_notified FROM alert_states a
		JOIN hosts h ON h.tenant_key = a.tenant_key AND h.key = a.host_key
		WHERE a.tenant_key = ? ORDER BY a.rule_key, h.name`, tk)
	if err != nil {
		return nil, fmt.Errorf("list alert states: %w", err)
	}
	defer rows.Close()
	var out []domain.RuleHostState
	for rows.Next() {
		var st domain.RuleHostState
		var raw []byte
		var state string
		var since int64
		var notified sql.NullInt64
		if err := rows.Scan(&st.Rule, &raw, &state, &since, &notified); err != nil {
			return nil, fmt.Errorf("scan alert state: %w", err)
		}
		id, err := decID(raw)
		if err != nil {
			return nil, err
		}
		st.Host, st.State, st.Since = domain.HostID(id), domain.AlertState(state), decTime(since)
		st.LastNotified = decOptTime(notified)
		out = append(out, st)
	}
	return out, rows.Err()
}

// RecordTransition implements alerting.StateRepo: the event row first, then
// the state, in one transaction (R1).
func (s *Store) RecordTransition(ctx context.Context, sc domain.Scope, st domain.RuleHostState, ev *domain.AlertEvent) (int64, error) {
	var id int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		tk, err := tenantKey(ctx, tx, sc)
		if err != nil {
			return err
		}
		hk, err := hostKey(ctx, tx, tk, st.Host)
		if err != nil {
			return err
		}
		if err := ruleExists(ctx, tx, tk, st.Rule); err != nil {
			return err
		}
		if ev != nil {
			notify := 0
			if ev.Notify {
				notify = 1
			}
			res, err := tx.ExecContext(ctx, `INSERT INTO alert_events (tenant_key, rule_key, host_key, from_state, to_state,
				at, value, notify) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				tk, st.Rule, hk, string(ev.From), string(ev.To), encTime(ev.At), ev.Value, notify)
			if err != nil {
				return fmt.Errorf("insert alert event: %w", err)
			}
			if id, err = res.LastInsertId(); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO alert_states (tenant_key, rule_key, host_key, state, since, last_notified)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (rule_key, host_key) DO UPDATE SET state = excluded.state, since = excluded.since,
				last_notified = excluded.last_notified WHERE alert_states.tenant_key = excluded.tenant_key`,
			tk, st.Rule, hk, string(st.State), encTime(st.Since), encOptTime(st.LastNotified))
		if err != nil {
			return fmt.Errorf("upsert alert state: %w", err)
		}
		return nil
	})
	return id, err
}

// SetDelivery implements alerting.StateRepo.
func (s *Store) SetDelivery(ctx context.Context, sc domain.Scope, event int64, outcome, delivery string) error {
	w := s.db.Writer()
	tk, err := tenantKey(ctx, w, sc)
	if err != nil {
		return err
	}
	res, err := w.ExecContext(ctx, `UPDATE alert_events SET outcome = ?, delivery = ? WHERE tenant_key = ? AND key = ?`,
		outcome, delivery, tk, event)
	if err != nil {
		return fmt.Errorf("record delivery: %w", err)
	}
	return mustAffect(res, "alert event")
}

// ListEvents implements alerting.StateRepo, newest first.
func (s *Store) ListEvents(ctx context.Context, sc domain.Scope, limit int) ([]domain.AlertEvent, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	rows, err := r.QueryContext(ctx, `SELECT e.key, e.rule_key, h.id, e.from_state, e.to_state, e.at, e.value, e.notify,
		e.outcome, e.delivery FROM alert_events e
		JOIN hosts h ON h.tenant_key = e.tenant_key AND h.key = e.host_key
		WHERE e.tenant_key = ? ORDER BY e.key DESC LIMIT ?`, tk, limit)
	if err != nil {
		return nil, fmt.Errorf("list alert events: %w", err)
	}
	defer rows.Close()
	var out []domain.AlertEvent
	for rows.Next() {
		var ev domain.AlertEvent
		var raw []byte
		var from, to string
		var at, notify int64
		var value sql.NullFloat64
		if err := rows.Scan(&ev.ID, &ev.Rule, &raw, &from, &to, &at, &value, &notify, &ev.Outcome, &ev.Delivery); err != nil {
			return nil, fmt.Errorf("scan alert event: %w", err)
		}
		id, err := decID(raw)
		if err != nil {
			return nil, err
		}
		ev.Host, ev.From, ev.To, ev.At, ev.Notify = domain.HostID(id), domain.AlertState(from), domain.AlertState(to), decTime(at), notify == 1
		if value.Valid {
			ev.Value = &value.Float64
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func encOptTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return encTime(*t)
}

func decOptTime(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := decTime(v.Int64)
	return &t
}
