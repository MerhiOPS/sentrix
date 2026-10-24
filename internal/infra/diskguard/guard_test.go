package diskguard

import (
	"errors"
	"testing"
	"time"
)

func TestGuardWatermarkCachingAndErrors(t *testing.T) {
	now := time.Now()
	free := uint64(50)
	var fail bool
	g := New("/x", 0.2, 10*time.Second)
	g.now = func() time.Time { return now }
	g.usage = func(string) (uint64, uint64, error) {
		if fail {
			return 0, 0, errors.New("statfs failed")
		}
		return free, 100, nil
	}
	if !g.LogsAllowed() {
		t.Fatal("50% free refused")
	}
	free = 10
	if !g.LogsAllowed() {
		t.Fatal("re-checked inside the interval")
	}
	now = now.Add(11 * time.Second)
	if g.LogsAllowed() {
		t.Fatal("10% free allowed (watermark 20%)")
	}
	fail = true
	now = now.Add(11 * time.Second)
	if g.LogsAllowed() {
		t.Fatal("a failed check changed the answer")
	}
	fail, free = false, 30
	now = now.Add(11 * time.Second)
	if !g.LogsAllowed() {
		t.Fatal("did not resume once space recovered")
	}
}
