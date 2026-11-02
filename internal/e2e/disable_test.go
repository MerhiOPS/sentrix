package e2e

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/agent"
	"github.com/MerhiOPS/sentrix/internal/domain"
	"github.com/MerhiOPS/sentrix/internal/infra/tlscert"
	"github.com/MerhiOPS/sentrix/internal/wire"
)

// TestDisabledHostRefusedDespiteCachedCredential: the credential cache
// (1 minute here) is warm when the host is disabled directly in the
// database, as a separate process would, without invalidating the cache.
// The next batch is refused with 403 and stores nothing.
func TestDisabledHostRefusedDespiteCachedCredential(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	id, err := agent.Enroll(ctx, t.TempDir(), s.srv.URL, s.token(t, s.pin), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	host, _ := domain.ParseHostID(id.HostID)
	tlsConf, err := tlscert.PinnedClientConfig(s.pin) // as the agent connects
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConf}}
	send := func(seq uint64) int {
		req, _ := http.NewRequest(http.MethodPost, s.srv.URL+wire.PathBatch, bytes.NewReader(typicalBatch(seq, time.Now().Add(-time.Minute))))
		req.Header.Set("Authorization", "Bearer "+id.Credential)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Encoding", "gzip")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		return res.StatusCode
	}
	if code := send(1); code != http.StatusOK {
		t.Fatalf("first batch: %d", code)
	}
	now := time.Now()
	if err := s.store.SetHostDisabled(ctx, s.scope, host, &now); err != nil { // no cache invalidation
		t.Fatal(err)
	}
	before, _ := s.store.RawCore(ctx, s.scope, host, now.Add(-time.Hour), now.Add(time.Hour))
	if code := send(2); code != http.StatusForbidden {
		t.Fatalf("batch from a disabled host with a cached credential: %d, want 403", code)
	}
	after, _ := s.store.RawCore(ctx, s.scope, host, now.Add(-time.Hour), now.Add(time.Hour))
	if len(after) != len(before) {
		t.Fatalf("a disabled host stored samples: %d → %d", len(before), len(after))
	}
}
