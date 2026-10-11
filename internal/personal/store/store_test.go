package store

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "db", "sessions.db"))
	assert.Require(t, assert.NoErr(t, err))
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenCreatesSchemaAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db", "sessions.db")
	s, err := Open(path)
	assert.Require(t, assert.NoErr(t, err))
	for _, tbl := range []string{"sessions", "entries", "turns", "events", "entries_fts", "memory_meta"} {
		var n int
		assert.NoErr(t, s.db.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&n))
		assert.Eq(t, 1, n)
	}
	assert.NoErr(t, s.Close())

	s2, err := Open(path)
	assert.Require(t, assert.NoErr(t, err))
	defer s2.Close()
	var n int
	assert.NoErr(t, s2.db.QueryRow(`SELECT COUNT(1) FROM schema_migrations`).Scan(&n))
	assert.Eq(t, len(migrationNames(t)), n)

	// Only sessions.db and its WAL companions are produced.
	ents, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range ents {
		assert.Contains(t, []string{"sessions.db", "sessions.db-wal", "sessions.db-shm"}, e.Name())
	}
}

func TestPragmasApplyToEveryPooledConnection(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	// Hold several connections open simultaneously so the pool must create new ones.
	var conns []interface{ Close() error }
	for i := 0; i < 4; i++ {
		c, err := s.db.Conn(ctx)
		assert.Require(t, assert.NoErr(t, err))
		conns = append(conns, c)
		var mode string
		var busy, fk int
		assert.NoErr(t, c.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode))
		assert.NoErr(t, c.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busy))
		assert.NoErr(t, c.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk))
		assert.Eq(t, "wal", mode)
		assert.Eq(t, 5000, busy)
		assert.Eq(t, 1, fk)
	}
	for _, c := range conns {
		_ = c.Close()
	}
}

func TestConcurrentWritersFromTwoStoresDoNotLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	a, err := Open(path)
	assert.Require(t, assert.NoErr(t, err))
	defer a.Close()
	b, err := Open(path)
	assert.Require(t, assert.NoErr(t, err))
	defer b.Close()

	var wg sync.WaitGroup
	errs := make(chan error, 200)
	for _, s := range []*Store{a, b} {
		wg.Add(1)
		go func(s *Store) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_, err := s.db.Exec(`INSERT INTO events(session_id, kind, started_at) VALUES ('s','model','t')`)
				if err != nil {
					errs <- err
				}
			}
		}(s)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("write failed: %v", err)
	}
	var n int
	assert.NoErr(t, a.db.QueryRow(`SELECT COUNT(1) FROM events`).Scan(&n))
	assert.Eq(t, 100, n)
}
