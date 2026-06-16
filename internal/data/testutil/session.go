package testutil

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/inhere/suag/internal/data"
)

// NewMigratedSQLitePath creates a temporary SQLite database and applies the
// repository migrations from db/migrations.
func NewMigratedSQLitePath(t testing.TB, name string) string {
	t.Helper()
	if name == "" {
		name = "test.db"
	}

	dbPath := filepath.Join(t.TempDir(), name)
	ApplyHostMigrations(t, dbPath)
	return dbPath
}

// ApplyHostMigrations applies the current Host repository migrations to dbPath.
func ApplyHostMigrations(t testing.TB, dbPath string) {
	t.Helper()

	db, err := data.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("data.OpenSQLite() error = %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("db.Close() error = %v", err)
		}
	}()

	if err := data.ApplyMigrationUps(context.Background(), db, hostMigrationDir(t)); err != nil {
		t.Fatalf("data.ApplyMigrationUps() error = %v", err)
	}
}

func hostMigrationDir(t testing.TB) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "db", "migrations")
}
