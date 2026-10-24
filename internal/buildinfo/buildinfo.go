// Package buildinfo describes the running binary: its version and the
// commit it was built from, read from the module build information Go
// embeds (-buildvcs), so a release cannot report the wrong commit.
package buildinfo

import (
	"runtime/debug"
	"strings"
)

// Describe renders "<version> (<commit>[, modified]) <go version>".
func Describe(version string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version + " (no build info)"
	}
	return version + " (" + commit(info.Settings) + ") " + info.GoVersion
}

func commit(settings []debug.BuildSetting) string {
	var rev, modified string
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if rev == "" {
		return "commit unknown"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if strings.EqualFold(modified, "true") {
		rev += ", modified"
	}
	return rev
}
