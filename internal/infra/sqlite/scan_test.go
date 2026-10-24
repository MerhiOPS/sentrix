package sqlite

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestScanFileFindsNeedlesAcrossChunkEdges(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	data := bytes.Repeat([]byte{'.'}, scanChunk-3)
	data = append(data, []byte("NEEDLE-ACROSS")...) // starts 3 bytes before the first chunk ends
	data = append(data, bytes.Repeat([]byte{'.'}, 100)...)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := scanFile(p, [][]byte{[]byte("NEEDLE-ACROSS"), []byte("absent")})
	if err != nil || got[0] != int64(scanChunk-3) || got[1] != -1 {
		t.Fatalf("found %v, %v", got, err)
	}
}
