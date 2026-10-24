package tlscert

import (
	"bytes"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadOrCreateIsStableAndPrivate(t *testing.T) {
	dir := t.TempDir()
	c1, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	p1, _ := LeafPin(c1)
	p2, _ := LeafPin(c2)
	if !bytes.Equal(p1, p2) {
		t.Fatal("second boot produced a different key: every agent pin would break")
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(filepath.Join(dir, keyFile))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("key file mode %v, want 0600", fi.Mode().Perm())
		}
	}
}

func serve(t *testing.T, dir string) *httptest.Server {
	t.Helper()
	c, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	srv.TLS = ServerConfig(c)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string, pin []byte) error {
	t.Helper()
	cfg, err := PinnedClientConfig(pin)
	if err != nil {
		t.Fatal(err)
	}
	cl := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg, Proxy: nil, DialContext: (&net.Dialer{}).DialContext}}
	resp, err := cl.Get(url)
	if err == nil {
		_ = resp.Body.Close()
	}
	return err
}

func TestPinnedClientAcceptsOnlyThePinnedKey(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	srvA := serve(t, dirA)
	cA, _ := LoadOrCreate(dirA)
	cB, _ := LoadOrCreate(dirB)
	pinA, _ := LeafPin(cA)
	pinB, _ := LeafPin(cB)

	if err := get(t, srvA.URL, pinA); err != nil {
		t.Fatalf("correct pin rejected: %v", err)
	}
	err := get(t, srvA.URL, pinB)
	if err == nil {
		t.Fatal("a different server key was accepted")
	}
	if !errors.Is(err, ErrPinMismatch) {
		t.Fatalf("mismatch error not identifiable: %v", err)
	}
}

func TestPinnedClientConfigRejectsBadPinLength(t *testing.T) {
	if _, err := PinnedClientConfig(make([]byte, 16)); err == nil {
		t.Fatal("16-byte pin accepted")
	}
}
