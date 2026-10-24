package ingesthttp

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/wire"
)

func TestLimiterRefillsAndIsPerHost(t *testing.T) {
	now := now0
	l := newLimiter(2, 3, func() time.Time { return now })
	a, b := domain.NewHostID(), domain.NewHostID()
	for i := range 3 {
		if ok, _ := l.allow(a); !ok {
			t.Fatalf("request %d inside the burst refused", i)
		}
	}
	ok, wait := l.allow(a)
	if ok || wait != 500*time.Millisecond {
		t.Fatalf("over the burst: ok %v wait %v, want refused for 500ms at 2/s", ok, wait)
	}
	if ok, _ := l.allow(b); !ok {
		t.Fatal("one host's burst limited another host")
	}
	now = now.Add(time.Second)
	if ok, _ := l.allow(a); !ok {
		t.Fatal("tokens did not refill")
	}
	if retryAfterSeconds(10*time.Millisecond) != 1 || retryAfterSeconds(1500*time.Millisecond) != 2 {
		t.Fatal("Retry-After must be whole seconds, at least 1")
	}
}

// Over the cap: 429 with Retry-After, and nothing stored — the agent keeps
// the batch and resends it.
func TestRateLimitedBatchIs429AndStoresNothing(t *testing.T) {
	r := newRig(t)
	r.h.(*Handler).limit = newLimiter(1, 2, func() time.Time { return now0 })
	cred, er := r.enrolled(t)
	codes := []int{}
	for seq := uint64(1); seq <= 3; seq++ {
		rec := do(t, r.h, wire.PathBatch, cred, gzipped(t, batchBody(t, seq, int(seq))), true)
		codes = append(codes, rec.Code)
		if rec.Code == http.StatusTooManyRequests && rec.Header().Get("Retry-After") != "1" {
			t.Fatalf("Retry-After = %q", rec.Header().Get("Retry-After"))
		}
	}
	if codes[0] != 200 || codes[1] != 200 || codes[2] != http.StatusTooManyRequests {
		t.Fatalf("codes %v, want 200 200 429", codes)
	}
	hid, _ := domain.ParseHostID(er.HostID)
	h, _ := r.store.GetHost(context.Background(), r.scope, hid)
	ss, _ := r.store.RawCore(context.Background(), r.scope, hid, now0.Add(-time.Hour), now0.Add(time.Hour))
	if len(ss) != 2 || h.LastSeen.IsZero() {
		t.Fatalf("stored %d samples, want 2: the refused third batch (a new, older slot) must store nothing", len(ss))
	}
}
