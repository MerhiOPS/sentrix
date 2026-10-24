//go:build !windows

package logtail

import (
	"fmt"
	"os"
	"syscall"
)

// openShared opens a log file for reading. On Unix a retained descriptor
// never blocks the writer's rename or unlink.
func openShared(path string) (*os.File, error) { return os.Open(path) } //nolint:gosec // paths come from the operator's globs

// fileKey identifies the file behind f across renames: device + inode.
func fileKey(f *os.File) (string, error) {
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	return statKey(fi)
}

// pathKey identifies the file currently at path.
func pathKey(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return statKey(fi)
}

func statKey(fi os.FileInfo) (string, error) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("no inode for %s", fi.Name())
	}
	return fmt.Sprintf("%d:%d", st.Dev, st.Ino), nil //nolint:unconvert // Dev's type differs per OS
}
