package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	gkconfig "github.com/gookit/config/v2"

	"github.com/inhere/suag/internal/data"
)

const defaultHostMigrationDir = "db/migrations"

func prepareHostSessionDatabase(ctx context.Context, dbPath string) error {
	migrationDir := resolveHostMigrationDir()

	db, err := data.OpenSQLite(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := data.ApplyMigrationUps(ctx, db, migrationDir); err != nil {
		return fmt.Errorf("apply host migrations: %w", err)
	}
	return nil
}

func resolveHostMigrationDir() string {
	raw := strings.TrimSpace(gkconfig.String("migrations.path"))
	if raw == "" {
		raw = defaultHostMigrationDir
	}
	dir := filepath.Clean(filepath.FromSlash(raw))
	if filepath.IsAbs(dir) {
		return dir
	}
	if _, err := os.Stat(dir); err == nil {
		return dir
	}
	if repoDir := sourceRepositoryRoot(); repoDir != "" {
		return filepath.Join(repoDir, dir)
	}
	return dir
}

func sourceRepositoryRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
