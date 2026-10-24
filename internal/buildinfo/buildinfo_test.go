package buildinfo

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestCommit(t *testing.T) {
	s := []debug.BuildSetting{{Key: "vcs.revision", Value: "d4b5a60aabbccddeeff00112233"}, {Key: "vcs.modified", Value: "true"}}
	if got := commit(s); got != "d4b5a60aabbc, modified" {
		t.Fatalf("commit = %q", got)
	}
	if got := commit(nil); got != "commit unknown" {
		t.Fatalf("no vcs info: %q", got)
	}
	if !strings.HasPrefix(Describe("v1.2.3"), "v1.2.3 (") {
		t.Fatal("Describe lost the version")
	}
}
