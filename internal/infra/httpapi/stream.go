package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/livehub"
)

const (
	subscribeTimeout = 10 * time.Second
	writeTimeout     = 10 * time.Second
	pingEvery        = 30 * time.Second
	maxSubscribeMsg  = 64 << 10
	maxSubscribeHost = 1000
)

type subscribeMsg struct {
	Type    string    `json:"type"`
	Hosts   *[]string `json:"hosts"` // null: every host of the tenant
	SinceMS int64     `json:"since_ms"`
}

// handleStream upgrades to a WebSocket fed by the live hub. The tenant comes
// from the session; the hub filters by it, so naming another tenant's host
// ids yields nothing — no error that would confirm they exist.
func (a *API) handleStream(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r, a.cfg.ExtraOrigins) {
		writeJSON(w, http.StatusForbidden, errorBody{Error: "cross-origin request refused"})
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true}) //nolint:gosec // Origin checked by sameOrigin above
	if err != nil {
		return // Accept has written the HTTP error
	}
	defer c.CloseNow() //nolint:errcheck // best effort on exit
	c.SetReadLimit(maxSubscribeMsg)
	sess := sessionFrom(r.Context())

	sub, err := a.readSubscribe(r.Context(), c, sess.Tenant)
	if err != nil {
		_ = c.Close(websocket.StatusPolicyViolation, "expected a subscribe message")
		return
	}
	defer a.cfg.Live.Unsubscribe(sub)
	ctx := c.CloseRead(r.Context()) // ends when the client goes away
	if err := pump(ctx, c, sub); err != nil && !errors.Is(err, context.Canceled) {
		slog.Debug("stream ended", "err", err)
	}
}

func (a *API) readSubscribe(ctx context.Context, c *websocket.Conn, tenant domain.TenantID) (*livehub.Sub, error) {
	rctx, cancel := context.WithTimeout(ctx, subscribeTimeout)
	defer cancel()
	_, data, err := c.Read(rctx)
	if err != nil {
		return nil, err
	}
	var m subscribeMsg
	if err := json.Unmarshal(data, &m); err != nil || m.Type != "subscribe" {
		return nil, errors.New("bad subscribe message")
	}
	var hosts []domain.HostID
	if m.Hosts != nil {
		hosts = []domain.HostID{} // explicit list: possibly empty, never "all"
		for i, s := range *m.Hosts {
			if i >= maxSubscribeHost {
				break
			}
			if id, err := domain.ParseHostID(s); err == nil {
				hosts = append(hosts, id)
			}
		}
	}
	return a.cfg.Live.Subscribe(tenant, hosts, time.UnixMilli(m.SinceMS)), nil
}

// pump writes hub events until the subscription closes (slow client) or the
// connection ends. A closed subscription closes the socket so the client
// reconnects with since_ms and the ring buffer fills the gap.
func pump(ctx context.Context, c *websocket.Conn, sub *livehub.Sub) error {
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
			if err := writeFrame(ctx, c, ev); err != nil {
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

func writeFrame(ctx context.Context, c *websocket.Conn, ev livehub.Event) error {
	frame := coreFrameDTO{Type: "core", HostID: ev.Host.String(), Samples: make([]sampleDTO, len(ev.Samples))}
	for i, s := range ev.Samples {
		frame.Samples[i] = toSample(s)
	}
	b, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return c.Write(wctx, websocket.MessageText, b)
}
