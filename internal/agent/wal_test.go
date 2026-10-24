package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func body(n int, fill byte) []byte { return bytes.Repeat([]byte{fill}, n) }

func openWAL(t *testing.T, dir string, maxBytes int64, c *clock) *WAL {
	t.Helper()
	w, err := OpenWAL(dir, maxBytes, time.Hour, c.now)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// A restart (the agent killed at any point) resends exactly what was not
// acknowledged, in order, byte-identical: the server dedupes by batch_seq,
// so nothing is lost and nothing is stored twice.
func TestWALSurvivesRestartInOrder(t *testing.T) {
	dir, c := t.TempDir(), &clock{time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}
	w := openWAL(t, dir, DefaultWALBytes, c)
	for seq := uint64(1); seq <= 3; seq++ {
		mustPush(t, w, Pending{Seq: seq, Body: body(100, byte(seq)), Samples: int(seq)})
	}
	w.Remove(1) // acknowledged
	// An interrupted write leaves a temp file; a torn file has no header.
	_ = os.WriteFile(filepath.Join(dir, walDirName, ".00000000000000000004.batch.tmp-1"), []byte("x"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, walDirName, "00000000000000000009.batch"), []byte("STX"), 0o600)

	w2 := openWAL(t, dir, DefaultWALBytes, c)
	if w2.Len() != 2 || w2.Dropped() != 1 {
		t.Fatalf("after restart: len %d dropped %d, want 2 batches and the torn file counted", w2.Len(), w2.Dropped())
	}
	for _, want := range []uint64{2, 3} {
		p, ok := w2.Head()
		if !ok || p.Seq != want || p.Samples != int(want) || !bytes.Equal(p.Body, body(100, byte(want))) {
			t.Fatalf("head %+v, want seq %d with its original body", p.Seq, want)
		}
		w2.Remove(p.Seq)
	}
	if left, _ := os.ReadDir(filepath.Join(dir, walDirName)); len(left) != 0 {
		t.Fatalf("files left after every batch was acknowledged: %d", len(left))
	}
}

func TestWALPastCapDropsOldestAndCounts(t *testing.T) {
	c := &clock{time.Now()}
	w := openWAL(t, t.TempDir(), 1000, c)
	for seq := uint64(1); seq <= 5; seq++ {
		mustPush(t, w, Pending{Seq: seq, Body: body(250, 'x')}) // ~300 bytes with the header
	}
	p, _ := w.Head()
	if w.Len() != 3 || w.Dropped() != 2 || p.Seq != 3 || w.Bytes() > 1000 {
		t.Fatalf("len %d dropped %d head %d bytes %d: want the two oldest dropped", w.Len(), w.Dropped(), p.Seq, w.Bytes())
	}
	if err := w.Push(Pending{Seq: 6, Body: body(2000, 'x')}); err == nil || w.Dropped() != 3 {
		t.Fatalf("a batch larger than the WAL: err %v dropped %d", err, w.Dropped())
	}
}

func TestWALDropsBatchesPastMaxAge(t *testing.T) {
	c := &clock{time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}
	w := openWAL(t, t.TempDir(), DefaultWALBytes, c)
	mustPush(t, w, Pending{Seq: 1, Body: body(10, 'a')})
	c.t = c.t.Add(30 * time.Minute)
	mustPush(t, w, Pending{Seq: 2, Body: body(10, 'b')})
	c.t = c.t.Add(31 * time.Minute) // seq 1 is now 61 minutes old
	if p, ok := w.Head(); !ok || p.Seq != 2 || w.Dropped() != 1 {
		t.Fatalf("head %d ok %v dropped %d: the server would refuse seq 1, it must be dropped locally", p.Seq, ok, w.Dropped())
	}
}

func TestWALDiscardCountsRemoveDoesNot(t *testing.T) {
	w := openWAL(t, t.TempDir(), DefaultWALBytes, &clock{time.Now()})
	mustPush(t, w, Pending{Seq: 1, Body: body(1, 'a')})
	mustPush(t, w, Pending{Seq: 2, Body: body(1, 'b')})
	w.Remove(2) // not the head: ignored
	w.Remove(1)
	w.Discard(2)
	if w.Len() != 0 || w.Dropped() != 1 {
		t.Fatalf("len %d dropped %d", w.Len(), w.Dropped())
	}
}
