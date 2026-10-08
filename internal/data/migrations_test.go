package data

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestParseMigrationFile(t *testing.T) {
	t.Run("valid miglite file", func(t *testing.T) {
		file, err := ParseMigrationFile("001-create.sql", "-- Migrate:UP\nCREATE TABLE demo(id TEXT);\n\n-- Migrate:DOWN\nDROP TABLE demo;")
		assert.NoErr(t, err)
		assert.Eq(t, "001-create.sql", file.Path)
		assert.Eq(t, "CREATE TABLE demo(id TEXT);", file.Up)
		assert.Eq(t, "DROP TABLE demo;", file.Down)
	})

	t.Run("missing down marker", func(t *testing.T) {
		_, err := ParseMigrationFile("bad.sql", "-- Migrate:UP\nSELECT 1;")
		assert.Err(t, err)
	})

	t.Run("missing up marker", func(t *testing.T) {
		_, err := ParseMigrationFile("bad.sql", "SELECT 1;\n-- Migrate:DOWN\nSELECT 0;")
		assert.Err(t, err)
	})
}

func TestApplyMigrationUps(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenSQLite(dbPath)
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, db.Close())
	})

	migrationDir := t.TempDir()
	migrationPath := filepath.Join(migrationDir, "20260501-100000-create-demo.sql")
	err = os.WriteFile(migrationPath, []byte("-- Migrate:UP\nCREATE TABLE IF NOT EXISTS demo(id TEXT PRIMARY KEY);\n\n-- Migrate:DOWN\nDROP TABLE IF EXISTS demo;"), 0o644)
	assert.NoErr(t, err)

	err = ApplyMigrationUps(context.Background(), db, migrationDir)
	assert.NoErr(t, err)

	_, err = db.Exec("INSERT INTO demo(id) VALUES ('one')")
	assert.NoErr(t, err)
}
