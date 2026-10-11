// Package store is the single-file SQLite core store for personal sessions:
// sessions, a linear entry log, turns (agentcore run state) and events.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

const (
	migrateUpMarker   = "-- Migrate:UP"
	migrateDownMarker = "-- Migrate:DOWN"
)

// timeLayout is the fixed-width UTC format used for every timestamp column, so
// lexical ordering equals chronological ordering.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timeLayout)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(timeLayout, s)
	return t
}

// Store wraps the shared *sql.DB of the core store.
type Store struct {
	db   *sql.DB
	path string
}

// buildDSN makes every pooled connection run the pragmas on open (modernc
// executes each _pragma value as "PRAGMA ..." for every new connection).
func buildDSN(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // "C:/x" -> "/C:/x" keeps the file: URI well-formed on Windows
	}
	u := url.URL{Path: p}
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	return "file://" + u.EscapedPath() + "?" + q.Encode()
}

// Open opens (creating if needed) the store at path and applies pending
// migrations. Repeated opens are idempotent.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create store directory: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", buildDSN(abs))
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open store: %w", err)
	}
	s := &Store{db: db, path: abs}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the handle for read-only queries by sibling packages.
func (s *Store) DB() *sql.DB { return s.db }

// Path returns the absolute database file path.
func (s *Store) Path() string { return s.path }

// migrate applies embedded UP sections in file-name order, recording each in
// schema_migrations so that a second Open skips them.
func (s *Store) migrate(ctx context.Context) error { return s.migrateUpTo(ctx, "") }

// migrateUpTo is migrate stopping after the file named last ("" = all); tests
// use it to build a database at an older schema version.
func (s *Store) migrateUpTo(ctx context.Context, last string) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	files, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".sql") {
			names = append(names, f.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if last != "" && name > last {
			break
		}
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM schema_migrations WHERE name = ?`, name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		raw, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		up, err := parseUp(name, string(raw))
		if err != nil {
			return err
		}
		// DDL and the ledger row commit together so a crash cannot half-apply.
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, up); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(name, applied_at) VALUES (?, ?)`, name, formatTime(time.Now())); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// parseUp extracts the UP section of a miglite-format file.
func parseUp(name, content string) (string, error) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, migrateUpMarker) {
		return "", fmt.Errorf("missing migration up marker in %s", name)
	}
	up := strings.TrimPrefix(content, migrateUpMarker)
	if i := strings.Index(up, migrateDownMarker); i >= 0 {
		up = up[:i]
	}
	up = strings.TrimSpace(up)
	if up == "" {
		return "", fmt.Errorf("empty migration up section in %s", name)
	}
	return up, nil
}
