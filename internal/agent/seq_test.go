package agent

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestSeqIsFlooredByClock(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	s, err := OpenSeqSource(t.TempDir(), fixedClock(now))
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.Next()
	if err != nil || v != uint64(now.UnixNano()) {
		t.Fatalf("first seq = %d (%v), want epoch ns %d", v, err, now.UnixNano())
	}
	w, _ := s.Next()
	if w != v+1 {
		t.Fatalf("same clock: second seq = %d, want %d", w, v+1)
	}
}

// TestFreshStateDirDoesNotRepeatSeq is the Phase 1 acceptance case: a lost
// state dir must not restart the sequence below what the server has seen,
// or every new batch would be acked as a replay and discarded.
func TestFreshStateDirDoesNotRepeatSeq(t *testing.T) {
	t1 := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	old, _ := OpenSeqSource(t.TempDir(), fixedClock(t1))
	var lastSeen uint64
	for i := 0; i < 100; i++ {
		lastSeen, _ = old.Next()
	}
	fresh, _ := OpenSeqSource(t.TempDir(), fixedClock(t1.Add(time.Second)))
	v, err := fresh.Next()
	if err != nil || v <= lastSeen {
		t.Fatalf("fresh state dir issued seq %d <= previously used %d", v, lastSeen)
	}
}

func TestSeqPersistsAcrossRestartAndClockStepBack(t *testing.T) {
	dir := t.TempDir()
	t1 := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	a, _ := OpenSeqSource(dir, fixedClock(t1))
	v1, _ := a.Next()
	// Restart with the clock stepped back an hour (NTP correction).
	b, err := OpenSeqSource(dir, fixedClock(t1.Add(-time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	v2, _ := b.Next()
	if v2 <= v1 {
		t.Fatalf("after restart with clock stepped back: %d <= %d", v2, v1)
	}
}

func TestSeqCorruptFileFallsBackToFloor(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, seqFile), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	s, err := OpenSeqSource(dir, fixedClock(now))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Next(); v != uint64(now.UnixNano()) {
		t.Fatalf("corrupt file: seq %d, want clock floor", v)
	}
}

func TestIdentityRoundTripAndPermissions(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadIdentity(dir); !errors.Is(err, ErrNotEnrolled) {
		t.Fatalf("empty dir: %v, want ErrNotEnrolled", err)
	}
	id := Identity{ServerURL: "https://s:8443", Credential: "c", HostID: "h",
		PinHex: "0000000000000000000000000000000000000000000000000000000000000000"}
	if err := SaveIdentity(dir, id); err != nil {
		t.Fatal(err)
	}
	got, err := LoadIdentity(dir)
	if err != nil || got != id {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	// Windows has no POSIX modes; its protection (DPAPI + ACL) is Phase 6.
	if fi, _ := os.Stat(filepath.Join(dir, identityFile)); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("identity file mode %v, want 0600", fi.Mode().Perm())
	}
}
