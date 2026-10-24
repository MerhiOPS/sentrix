package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// dial upgrades and returns the handshake status; the handshake response
// body is closed here so callers cannot leak it.
func (r *rig) dial(t *testing.T, cookie, origin string) (*websocket.Conn, int, error) {
	t.Helper()
	h := http.Header{}
	h.Set("Origin", origin)
	h.Set("Cookie", CookieName+"="+cookie)
	h.Set("Host", "sentrix.test")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, res, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(r.srv.URL, "http")+"/api/v1/stream",
		&websocket.DialOptions{HTTPHeader: h, Host: "sentrix.test"})
	status := 0
	if res != nil {
		status = res.StatusCode
		if res.Body != nil {
			_ = res.Body.Close()
		}
	}
	return c, status, err
}

func subscribe(t *testing.T, c *websocket.Conn, hosts string, since time.Time) {
	t.Helper()
	msg := `{"type":"subscribe","hosts":` + hosts + `,"since_ms":` + jsonInt(since.UnixMilli()) + `}`
	if err := c.Write(context.Background(), websocket.MessageText, []byte(msg)); err != nil {
		t.Fatal(err)
	}
}

func jsonInt(n int64) string { b, _ := json.Marshal(n); return string(b) }

// next reads one frame or returns false on timeout.
func next(t *testing.T, c *websocket.Conn, wait time.Duration) (coreFrameDTO, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		return coreFrameDTO{}, false
	}
	var f coreFrameDTO
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("frame: %v %s", err, data)
	}
	return f, true
}

func TestStreamDeliversOwnTenantAfterCommit(t *testing.T) {
	r := newRig(t)
	c, _, err := r.dial(t, r.login(t, r.a.viewer), testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow() //nolint:errcheck
	subscribe(t, c, "null", time.Now())
	time.Sleep(50 * time.Millisecond) // let the subscription register
	r.ingestSamples(t, r.a, 1, time.Now())
	f, ok := next(t, c, 2*time.Second)
	if !ok || f.Type != "core" || f.HostID != r.a.host.String() || len(f.Samples) != 1 || f.Samples[0].CPU != 42 {
		t.Fatalf("frame: %+v ok=%v", f, ok)
	}
}

func TestStreamGivesNoFramesForAnotherTenantsHost(t *testing.T) {
	r := newRig(t)
	c, _, err := r.dial(t, r.login(t, r.b.viewer), testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()                                        //nolint:errcheck
	subscribe(t, c, `["`+r.a.host.String()+`"]`, time.Time{}) // B names A's host
	time.Sleep(50 * time.Millisecond)
	r.ingestSamples(t, r.a, 1, time.Now())
	if f, ok := next(t, c, 500*time.Millisecond); ok {
		t.Fatalf("tenant B received a frame for tenant A's host: %+v", f)
	}
}

func TestStreamRefusesForeignOriginAndNoSession(t *testing.T) {
	r := newRig(t)
	if _, status, err := r.dial(t, r.login(t, r.a.viewer), "https://evil.example"); err == nil || status != http.StatusForbidden {
		t.Fatalf("foreign origin upgrade: err=%v status=%d", err, status)
	}
	if _, status, err := r.dial(t, "bogus", testOrigin); err == nil || status != http.StatusUnauthorized {
		t.Fatalf("no session upgrade: err=%v status=%d", err, status)
	}
}

func TestStreamBackfillSinceOnReconnect(t *testing.T) {
	r := newRig(t)
	now := time.Now().Truncate(5 * time.Second)
	r.ingestSamples(t, r.a, 1, now.Add(-15*time.Second), now.Add(-10*time.Second), now.Add(-5*time.Second))
	c, _, err := r.dial(t, r.login(t, r.a.viewer), testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()                                //nolint:errcheck
	subscribe(t, c, "null", now.Add(-12*time.Second)) // client already has up to -12s
	f, ok := next(t, c, 2*time.Second)
	if !ok || len(f.Samples) != 2 || f.Samples[0].TSMS != now.Add(-10*time.Second).UnixMilli() {
		t.Fatalf("backfill: %+v ok=%v, want the two samples after since", f, ok)
	}
}
