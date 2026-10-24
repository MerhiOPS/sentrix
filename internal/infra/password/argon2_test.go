package password

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestHashVerify(t *testing.T) {
	a := New(2)
	ctx := context.Background()
	h, err := a.Hash(ctx, "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=47104,t=1,p=1$") {
		t.Fatalf("unexpected encoding: %s", h)
	}
	if ok, err := a.Verify(ctx, "correct horse battery staple", h); err != nil || !ok {
		t.Fatalf("right password: %v %v", ok, err)
	}
	if ok, _ := a.Verify(ctx, "correct horse battery stapl", h); ok {
		t.Fatal("wrong password verified")
	}
	h2, _ := a.Hash(ctx, "correct horse battery staple")
	if h2 == h {
		t.Fatal("two hashes of one password are equal: salt not random")
	}
}

func TestVerifyRefusesTamperedParameters(t *testing.T) {
	a := New(1)
	for _, bad := range []string{
		"", "plaintext", "$argon2i$v=19$m=47104,t=1,p=1$c2FsdHNhbHQ$a2V5a2V5a2V5a2V5a2V5",
		"$argon2id$v=19$m=4194304,t=1,p=1$c2FsdHNhbHQ$a2V5a2V5a2V5a2V5a2V5", // 4 GiB: refused, not allocated
		"$argon2id$v=19$m=47104,t=99,p=1$c2FsdHNhbHQ$a2V5a2V5a2V5a2V5a2V5",
	} {
		if _, err := a.Verify(context.Background(), "x", bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestSemaphoreRespectsContext(t *testing.T) {
	a := New(1)
	a.sem <- struct{}{} // occupy the only slot
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := a.Hash(ctx, "whatever-password"); err == nil {
		t.Fatal("hash proceeded past a full semaphore")
	}
}
