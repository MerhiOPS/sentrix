package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/wire"
)

func TestClassifyContract(t *testing.T) {
	for status, want := range map[int]Outcome{
		200: Acked, 400: Dropped, 413: Dropped, 422: Dropped,
		401: Stopped, 403: Stopped,
		0: Retry, 429: Retry, 500: Retry, 502: Retry, 503: Retry, 404: Retry, 418: Retry,
	} {
		if got := classify(status); got != want {
			t.Errorf("classify(%d) = %s, want %s", status, got, want)
		}
	}
}

func TestBackoffIsBoundedAndJittered(t *testing.T) {
	b := NewBackoff(time.Second, 10*time.Second)
	b.rnd = func() float64 { return 0.999 }
	var last time.Duration
	for i := 0; i < 40; i++ {
		last = b.Next()
		if last > 10*time.Second {
			t.Fatalf("attempt %d waited %v, max 10s", i, last)
		}
	}
	if last < 9*time.Second {
		t.Fatalf("backoff never reached its ceiling: %v", last)
	}
	b.rnd = func() float64 { return 0 }
	if b.Next() != 0 {
		t.Fatal("full jitter must allow a zero wait")
	}
	b.Reset()
	b.rnd = func() float64 { return 0.999 }
	if w := b.Next(); w > time.Second {
		t.Fatalf("after Reset the wait is %v, want <= base", w)
	}
}

// scripted serves batch responses from a list of statuses and records seqs.
type scripted struct {
	mu       sync.Mutex
	statuses []int
	seqs     []uint64
}

func (s *scripted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	var seq uint64
	if p, err := gunzipBatch(body); err == nil {
		seq = p.BatchSeq
	}
	s.seqs = append(s.seqs, seq)
	st := s.statuses[0]
	if len(s.statuses) > 1 {
		s.statuses = s.statuses[1:]
	}
	w.WriteHeader(st)
	if st == http.StatusOK {
		_ = json.NewEncoder(w).Encode(wire.Ack{BatchSeq: seq, ServerTSMS: time.Now().UnixMilli()})
	}
}

func newTestShipper(t *testing.T, statuses ...int) (*Shipper, *WAL, *scripted) {
	t.Helper()
	srv := &scripted{statuses: statuses}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	c, _ := NewClient(ts.URL, nil)
	buf, err := OpenWAL(t.TempDir(), DefaultWALBytes, DefaultWALAge, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	s := NewShipper(c, Identity{ServerURL: ts.URL, Credential: "x"}, buf, slog.New(slog.DiscardHandler))
	s.backoff.rnd = func() float64 { return 0 }
	return s, buf, srv
}

func pending(t *testing.T, seq uint64) Pending {
	t.Helper()
	body, err := encodeBatch(wire.Batch{BatchSeq: seq})
	if err != nil {
		t.Fatal(err)
	}
	return Pending{Seq: seq, Body: body}
}

func TestShipperRetriesSameBatchThenAcks(t *testing.T) {
	s, buf, srv := newTestShipper(t, 503, 500, 0, 200)
	srv.statuses[2] = http.StatusBadGateway
	mustPush(t, buf, pending(t, 7))
	for i := 0; i < 4 && buf.Len() > 0; i++ {
		s.handle(context.Background(), pending(t, 7))
	}
	if buf.Len() != 0 {
		t.Fatal("batch not removed after the ack")
	}
	for _, seq := range srv.seqs {
		if seq != 7 {
			t.Fatalf("retries changed the batch: seqs %v", srv.seqs)
		}
	}
}

func TestShipperDropsInvalidAndKeepsOnStop(t *testing.T) {
	s, buf, _ := newTestShipper(t, 422)
	mustPush(t, buf, pending(t, 1))
	s.handle(context.Background(), pending(t, 1))
	if buf.Len() != 0 || buf.Dropped() != 1 {
		t.Fatalf("422: len %d dropped %d, want dropped and counted", buf.Len(), buf.Dropped())
	}
	s2, buf2, _ := newTestShipper(t, 401)
	mustPush(t, buf2, pending(t, 2))
	if wait := s2.handle(context.Background(), pending(t, 2)); wait != stoppedRecheck || buf2.Len() != 1 {
		t.Fatalf("401: wait %v len %d, want batch kept and recheck later", wait, buf2.Len())
	}
}

func TestDrainStopsOnFirstFailure(t *testing.T) {
	s, buf, _ := newTestShipper(t, 200, 503)
	mustPush(t, buf, pending(t, 1))
	mustPush(t, buf, pending(t, 2))
	if left := s.Drain(context.Background()); left != 1 {
		t.Fatalf("Drain left %d batches, want 1 (gives up on the 503, no retry loop)", left)
	}
}

func mustPush(t *testing.T, w *WAL, p Pending) {
	t.Helper()
	if err := w.Push(p); err != nil {
		t.Fatal(err)
	}
}
