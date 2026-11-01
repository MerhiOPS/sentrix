package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

const stateColumns = `key, status, since, last_check, failures, first_failure, cert, cert_expiry, revision`

// stateRow is one monitors row's state columns, in stateColumns order.
type stateRow struct {
	st                  domain.MonitorState
	status, cert        string
	since               int64
	last, first, expiry sql.NullInt64
}

func (r *stateRow) dest() []any {
	return []any{&r.st.Monitor, &r.status, &r.since, &r.last, &r.st.Failures, &r.first, &r.cert, &r.expiry, &r.st.Revision}
}

func (r *stateRow) state() domain.MonitorState {
	st := r.st
	st.Status, st.Cert, st.Since = domain.MonitorStatus(r.status), domain.CertStatus(r.cert), decTime(r.since)
	if r.last.Valid {
		st.LastCheck = decTime(r.last.Int64)
	}
	if r.first.Valid {
		st.FirstFailure = decTime(r.first.Int64)
	}
	st.CertExpiry = decOptTime(r.expiry)
	return st
}

// MonitorStates implements uptime.StateRepo, ordered by monitor.
func (s *Store) MonitorStates(ctx context.Context, sc domain.Scope) ([]domain.MonitorState, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	rows, err := r.QueryContext(ctx, `SELECT `+stateColumns+` FROM monitors WHERE tenant_key = ? ORDER BY key`, tk)
	if err != nil {
		return nil, fmt.Errorf("list monitor states: %w", err)
	}
	defer rows.Close()
	var out []domain.MonitorState
	for rows.Next() {
		var sr stateRow
		if err := rows.Scan(sr.dest()...); err != nil {
			return nil, fmt.Errorf("scan monitor state: %w", err)
		}
		out = append(out, sr.state())
	}
	return out, rows.Err()
}

// SetMonitorDelivery implements uptime.StateRepo.
func (s *Store) SetMonitorDelivery(ctx context.Context, sc domain.Scope, event int64, outcome, delivery string) error {
	w := s.db.Writer()
	tk, err := tenantKey(ctx, w, sc)
	if err != nil {
		return err
	}
	res, err := w.ExecContext(ctx, `UPDATE monitor_events SET outcome = ?, delivery = ? WHERE tenant_key = ? AND key = ?`,
		outcome, delivery, tk, event)
	if err != nil {
		return fmt.Errorf("record monitor delivery: %w", err)
	}
	return mustAffect(res, "monitor event")
}

// ListMonitorEvents implements uptime.StateRepo, newest first.
func (s *Store) ListMonitorEvents(ctx context.Context, sc domain.Scope, monitor int64, limit int) ([]domain.MonitorEvent, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	rows, err := r.QueryContext(ctx, `SELECT key, monitor_key, kind, at, class, down_for_ms, cert_expiry, notify, outcome,
		delivery FROM monitor_events WHERE tenant_key = ?1 AND (?2 = 0 OR monitor_key = ?2) ORDER BY key DESC LIMIT ?3`,
		tk, monitor, limit)
	if err != nil {
		return nil, fmt.Errorf("list monitor events: %w", err)
	}
	defer rows.Close()
	var out []domain.MonitorEvent
	for rows.Next() {
		var ev domain.MonitorEvent
		var kind, class string
		var at, notify int64
		var downFor, expiry sql.NullInt64
		if err := rows.Scan(&ev.ID, &ev.Monitor, &kind, &at, &class, &downFor, &expiry, &notify, &ev.Outcome, &ev.Delivery); err != nil {
			return nil, fmt.Errorf("scan monitor event: %w", err)
		}
		ev.Kind, ev.Class, ev.At, ev.Notify = domain.MonitorEventKind(kind), domain.ErrorClass(class), decTime(at), notify == 1
		ev.DownFor, ev.CertExpiry = time.Duration(downFor.Int64)*time.Millisecond, decOptTime(expiry)
		out = append(out, ev)
	}
	return out, rows.Err()
}

// MonitorResults implements uptime.HistoryReader.
func (s *Store) MonitorResults(ctx context.Context, sc domain.Scope, monitor int64, from, to time.Time, limit int) ([]domain.CheckResult, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	rows, err := r.QueryContext(ctx, `SELECT at, outcome, class, latency_ms, ttfb_ms, status, loss, cert_expiry FROM
		(SELECT * FROM monitor_results WHERE tenant_key = ? AND monitor_key = ? AND at >= ? AND at < ? ORDER BY at DESC LIMIT ?)
		ORDER BY at`, tk, monitor, encTime(from), encTime(to), limit)
	if err != nil {
		return nil, fmt.Errorf("read check results: %w", err)
	}
	defer rows.Close()
	var out []domain.CheckResult
	for rows.Next() {
		var c domain.CheckResult
		var at int64
		var outcome, class string
		var latency, ttfb, status, loss, expiry sql.NullInt64
		if err := rows.Scan(&at, &outcome, &class, &latency, &ttfb, &status, &loss, &expiry); err != nil {
			return nil, fmt.Errorf("scan check result: %w", err)
		}
		c.At, c.Outcome, c.Class = decTime(at), domain.CheckOutcome(outcome), domain.ErrorClass(class)
		c.Latency, c.TTFB = time.Duration(latency.Int64)*time.Millisecond, time.Duration(ttfb.Int64)*time.Millisecond
		c.StatusCode, c.Loss, c.CertExpiry = int(status.Int64), int(loss.Int64), decOptTime(expiry)
		out = append(out, c)
	}
	return out, rows.Err()
}

// MonitorHours implements uptime.HistoryReader.
func (s *Store) MonitorHours(ctx context.Context, sc domain.Scope, monitor int64, from, to time.Time) ([]domain.MonitorHour, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	rows, err := r.QueryContext(ctx, `SELECT monitor_key, hour, checks, failures, up_ms, down_ms, latency_sum_ms, latency_n,
		latency_max_ms FROM monitor_hourly WHERE tenant_key = ?1 AND (?2 = 0 OR monitor_key = ?2) AND hour >= ?3 AND hour < ?4
		ORDER BY hour, monitor_key`, tk, monitor, encTime(from), encTime(to))
	if err != nil {
		return nil, fmt.Errorf("read monitor hours: %w", err)
	}
	defer rows.Close()
	var out []domain.MonitorHour
	ms := func(v int64) time.Duration { return time.Duration(v) * time.Millisecond }
	for rows.Next() {
		var h domain.MonitorHour
		var hour, up, down, sum, maxLat int64
		if err := rows.Scan(&h.Monitor, &hour, &h.Checks, &h.Failures, &up, &down, &sum, &h.LatencyN, &maxLat); err != nil {
			return nil, fmt.Errorf("scan monitor hour: %w", err)
		}
		h.Hour, h.Up, h.Down, h.LatencySum, h.LatencyMax = decTime(hour), ms(up), ms(down), ms(sum), ms(maxLat)
		out = append(out, h)
	}
	return out, rows.Err()
}

// MonitorIncidents implements uptime.HistoryReader, newest first.
func (s *Store) MonitorIncidents(ctx context.Context, sc domain.Scope, monitor int64, limit int) ([]domain.Incident, error) {
	r := s.db.Reader()
	tk, err := tenantKey(ctx, r, sc)
	if err != nil {
		return nil, err
	}
	rows, err := r.QueryContext(ctx, `SELECT key, monitor_key, started, ended, class, failures FROM monitor_incidents
		WHERE tenant_key = ? AND monitor_key = ? ORDER BY started DESC, key DESC LIMIT ?`, tk, monitor, limit)
	if err != nil {
		return nil, fmt.Errorf("read incidents: %w", err)
	}
	defer rows.Close()
	var out []domain.Incident
	for rows.Next() {
		var in domain.Incident
		var started int64
		var ended sql.NullInt64
		var class string
		if err := rows.Scan(&in.ID, &in.Monitor, &started, &ended, &class, &in.Failures); err != nil {
			return nil, fmt.Errorf("scan incident: %w", err)
		}
		in.Start, in.End, in.Class = decTime(started), decOptTime(ended), domain.ErrorClass(class)
		out = append(out, in)
	}
	return out, rows.Err()
}
