package data

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	migrateUpMarker   = "-- Migrate:UP"
	migrateDownMarker = "-- Migrate:DOWN"
)

// MigrationFile is a parsed miglite-format SQL file.
type MigrationFile struct {
	Path string
	Up   string
	Down string
}

// LoadMigrationFiles reads miglite-format SQL files from migrationDir in
// lexical order. The format intentionally matches the miglite CLI files under
// db/migrations so tests and local bootstrap do not maintain another schema
// representation.
func LoadMigrationFiles(migrationDir string) ([]MigrationFile, error) {
	entries, err := filepath.Glob(filepath.Join(migrationDir, "*.sql"))
	if err != nil {
		return nil, fmt.Errorf("glob migration files: %w", err)
	}
	sort.Strings(entries)
	if len(entries) == 0 {
		return nil, fmt.Errorf("no migration files found in %s", migrationDir)
	}

	files := make([]MigrationFile, 0, len(entries))
	for _, migrationPath := range entries {
		content, err := os.ReadFile(migrationPath)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", migrationPath, err)
		}

		file, err := ParseMigrationFile(migrationPath, string(content))
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

// ParseMigrationFile extracts the UP and DOWN sections from a miglite-format
// migration file.
func ParseMigrationFile(path string, content string) (MigrationFile, error) {
	parts := strings.Split(content, migrateDownMarker)
	if len(parts) != 2 {
		return MigrationFile{}, fmt.Errorf("unexpected migration format in %s", path)
	}
	if !strings.HasPrefix(strings.TrimSpace(parts[0]), migrateUpMarker) {
		return MigrationFile{}, fmt.Errorf("missing migration up marker in %s", path)
	}

	upSQL := strings.TrimSpace(strings.TrimPrefix(parts[0], migrateUpMarker))
	if upSQL == "" {
		return MigrationFile{}, fmt.Errorf("empty migration up section in %s", path)
	}

	downSQL := strings.TrimSpace(parts[1])
	return MigrationFile{
		Path: path,
		Up:   upSQL,
		Down: downSQL,
	}, nil
}

// ApplyMigrationUps executes UP sections in lexical file order.
//
// This is a small bootstrap/test helper, not a replacement for the miglite
// migration ledger. Production upgrades should continue to run through miglite
// once migrations become non-idempotent.
func ApplyMigrationUps(ctx context.Context, db *sql.DB, migrationDir string) error {
	files, err := LoadMigrationFiles(migrationDir)
	if err != nil {
		return err
	}
	for _, file := range files {
		if _, err := db.ExecContext(ctx, file.Up); err != nil {
			return fmt.Errorf("apply migration %s: %w", file.Path, err)
		}
	}
	return nil
}
