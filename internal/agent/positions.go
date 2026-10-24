package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MerhiOPS/sentrix/internal/infra/logtail"
)

func positionFile(dir, source string) string { return filepath.Join(dir, "logpos-"+source+".json") }

// restorePositions loads each source's committed position.
func restorePositions(dir string, sources []logtail.Source) error {
	for _, s := range sources {
		b, err := os.ReadFile(positionFile(dir, s.Name())) //nolint:gosec // state dir is operator-configured
		if errors.Is(err, os.ErrNotExist) {
			b = nil
		} else if err != nil {
			return fmt.Errorf("read %s position: %w", s.Name(), err)
		}
		if err := s.Restore(b); err != nil {
			return fmt.Errorf("restore %s position: %w", s.Name(), err)
		}
	}
	return nil
}

// commitPositions persists the marks a settled batch carried.
func commitPositions(dir string, sources map[string]logtail.Source, marks map[string]logtail.Mark) error {
	var errs []error
	for name, m := range marks {
		src, ok := sources[name]
		if !ok {
			continue
		}
		state, err := src.Commit(m)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		errs = append(errs, writeFileAtomic(positionFile(dir, name), state, 0o600))
	}
	return errors.Join(errs...)
}
