package ingest

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

type fakeCreds struct {
	hosts map[[sha256.Size]byte]AuthHost
	calls int
	err   error
}

func (f *fakeCreds) AuthenticateCredential(_ context.Context, h []byte) (AuthHost, error) {
	f.calls++
	if f.err != nil {
		return AuthHost{}, f.err
	}
	var k [sha256.Size]byte
	copy(k[:], h)
	ah, ok := f.hosts[k]
	if !ok {
		return AuthHost{}, domain.ErrNotFound
	}
	return ah, nil
}

func TestAuthenticatorCachesAndExpires(t *testing.T) {
	cred := []byte("credential-bytes-0123456789abcdef")
	ah := authHost()
	store := &fakeCreds{hosts: map[[sha256.Size]byte]AuthHost{sha256.Sum256(cred): ah}}
	now := t0
	a := NewAuthenticator(store, 30*time.Second, 10, func() time.Time { return now })

	for i := 0; i < 3; i++ {
		got, err := a.Authenticate(context.Background(), cred)
		if err != nil || got != ah {
			t.Fatalf("auth: %v %v", got, err)
		}
	}
	if store.calls != 1 {
		t.Fatalf("store called %d times, want 1 (cached)", store.calls)
	}
	now = now.Add(31 * time.Second)
	if _, err := a.Authenticate(context.Background(), cred); err != nil {
		t.Fatal(err)
	}
	if store.calls != 2 {
		t.Fatalf("expired entry not refreshed: calls=%d", store.calls)
	}
	a.Invalidate(ah.Host)
	if _, err := a.Authenticate(context.Background(), cred); err != nil || store.calls != 3 {
		t.Fatalf("Invalidate did not drop the entry: calls=%d err=%v", store.calls, err)
	}
}

func TestAuthenticatorUnknownVsStoreFailure(t *testing.T) {
	store := &fakeCreds{hosts: map[[sha256.Size]byte]AuthHost{}}
	a := NewAuthenticator(store, time.Minute, 10, func() time.Time { return t0 })
	if _, err := a.Authenticate(context.Background(), []byte("nope")); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("unknown credential: err = %v, want ErrUnauthenticated", err)
	}
	store.err = errors.New("database is locked")
	_, err := a.Authenticate(context.Background(), []byte("nope"))
	if err == nil || errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("store failure must not read as unauthenticated (agent would stop shipping): %v", err)
	}
}

func TestAuthenticatorBoundsMemory(t *testing.T) {
	store := &fakeCreds{hosts: map[[sha256.Size]byte]AuthHost{}}
	for i := 0; i < 50; i++ {
		store.hosts[sha256.Sum256([]byte{byte(i)})] = authHost()
	}
	a := NewAuthenticator(store, time.Hour, 8, func() time.Time { return t0 })
	for i := 0; i < 50; i++ {
		if _, err := a.Authenticate(context.Background(), []byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(a.cache); n > 8 {
		t.Fatalf("cache holds %d entries, max 8", n)
	}
}
