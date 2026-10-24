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

// subscribeMsg is the client's first message: "subscribe" for metrics,
// "tail" for logs (with levels and q as in GET /logs).
type subscribeMsg struct {
	Type    string    `json:"type"`
	Hosts   *[]string `json:"hosts"` // null: every host of the tenant
	SinceMS int64     `json:"since_ms"`
	Levels  []string  `json:"levels"`
	Q       string    `json:"q"`
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

	m, err := readFirst(r.Context(), c)
	if err != nil {
		_ = c.Close(websocket.StatusPolicyViolation, "expected a subscribe or tail message")
		return
	}
	ctx := c.CloseRead(r.Context()) // ends when the client goes away
	switch m.Type {
	case "subscribe":
		sub := a.cfg.Live.Subscribe(sess.Tenant, hostIDs(m.Hosts), time.UnixMilli(m.SinceMS))
		defer a.cfg.Live.Unsubscribe(sub)
		err = pump(ctx, c, sub)
	case "tail":
		err = a.tail(ctx, c, sess.Tenant, m)
	default:
		_ = c.Close(websocket.StatusPolicyViolation, "unknown message type")
		return
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.Debug("stream ended", "err", err)
	}
}

func readFirst(ctx context.Context, c *websocket.Conn) (subscribeMsg, error) {
	rctx, cancel := context.WithTimeout(ctx, subscribeTimeout)
	defer cancel()
	_, data, err := c.Read(rctx)
	if err != nil {
		return subscribeMsg{}, err
	}
	var m subscribeMsg
	if err := json.Unmarshal(data, &m); err != nil {
		return subscribeMsg{}, errors.New("bad first message")
	}
	return m, nil
}

// hostIDs parses the host list; nil means all hosts. Unparseable ids are
// skipped, and another tenant's ids simply never match in the hub.
func hostIDs(in *[]string) []domain.HostID {
	if in == nil {
		return nil
	}
	hosts := []domain.HostID{} // explicit list: possibly empty, never "all"
	for i, s := range *in {
		if i >= maxSubscribeHost {
			break
		}
		if id, err := domain.ParseHostID(s); err == nil {
			hosts = append(hosts, id)
		}
	}
	return hosts
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
