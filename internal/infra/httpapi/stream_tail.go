package httpapi

import (
	"context"
	"encoding/json"
	"time"

	"github.com/coder/websocket"

	"github.com/MerhiOPS/sentrix/internal/app/query"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/livehub"
)

// tail streams newly stored, redacted lines matching the client's filter.
// The filter uses the search semantics and is applied in the hub, so the
// socket carries only what was asked for (OLD B10).
func (a *API) tail(ctx context.Context, c *websocket.Conn, tenant domain.TenantID, m subscribeMsg) error {
	clauses, err := query.ParseSearch(m.Q)
	if err != nil {
		return c.Close(websocket.StatusPolicyViolation, err.Error())
	}
	var levels []domain.Level
	for _, l := range m.Levels {
		levels = append(levels, domain.ParseLevel(l))
	}
	sub := a.cfg.Live.SubscribeLogs(tenant, livehub.LogFilter{Hosts: hostIDs(m.Hosts), Levels: levels, Clauses: clauses})
	defer a.cfg.Live.UnsubscribeLogs(sub)
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-sub.Events():
			if !ok {
				return c.Close(websocket.StatusTryAgainLater, "fell behind; reconnect")
			}
			b, err := json.Marshal(logFrameDTO{Type: "logs", HostID: ev.Host.String(), Lines: toLiveLines(ev.Lines)})
			if err != nil {
				return err
			}
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err = c.Write(wctx, websocket.MessageText, b)
			cancel()
			if err != nil {
				return err
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.Ping(pctx)
			cancel()
			if err != nil {
				return err
			}
		}
	}
}
