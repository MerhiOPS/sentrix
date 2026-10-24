package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/MerhiOPS/sentrix/internal/infra/storetest"
)

func newMigratedStore(t *testing.T) *Store {
	t.Helper()
	db, err := OpenMigrated(context.Background(), filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeDB(t, db) })
	return NewStore(db)
}

func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Stores { return newMigratedStore(t) })
}
