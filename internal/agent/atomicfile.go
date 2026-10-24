// Package agent is the agent runtime: collect, batch, buffer and ship, with
// the server's response contract applied exactly (PLAN §2).
package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// writeFileAtomic replaces path with data so a crash leaves either the old
// or the new content, never a torn file: write a temp file in the same
// directory, fsync it, then rename over the target.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmp := f.Name()
	cleanup := func(e error) error { return errors.Join(e, os.Remove(tmp)) }
	if err := f.Chmod(perm); err != nil {
		return cleanup(errors.Join(fmt.Errorf("chmod temp file: %w", err), f.Close()))
	}
	if _, err := f.Write(data); err != nil {
		return cleanup(errors.Join(fmt.Errorf("write temp file: %w", err), f.Close()))
	}
	if err := f.Sync(); err != nil {
		return cleanup(errors.Join(fmt.Errorf("sync temp file: %w", err), f.Close()))
	}
	if err := f.Close(); err != nil {
		return cleanup(fmt.Errorf("close temp file: %w", err))
	}
	if err := os.Rename(tmp, path); err != nil {
		return cleanup(fmt.Errorf("replace %s: %w", filepath.Base(path), err))
	}
	return nil
}
