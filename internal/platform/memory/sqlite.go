package memory

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// SQLiteStore is a persistent MemoryStore backed by SQLite.
// It stores conversation messages and memory fragments.
// Vector search is stubbed — install sqlite-vec extension for ANN search.
type SQLiteStore struct {
	mu      sync.Mutex
	db      *sql.DB
	agentID string
	inmem   *InMemStore // short-term cache; long-term persists to SQLite
}

// NewSQLiteStore opens (or creates) a SQLite database at dbPath and initialises
// the schema. agentID scopes all stored records to a specific agent.
func NewSQLiteStore(dbPath, agentID string, cfg InMemConfig) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", dbPath, err)
	}

	// Enable WAL mode for concurrent reads.
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		return nil, fmt.Errorf("enable WAL: %w", err)
	}

	if err := initSchema(db); err != nil {
		return nil, err
	}

	return &SQLiteStore{
		db:      db,
		agentID: agentID,
		inmem:   NewInMemStore(cfg),
	}, nil
}

// initSchema creates the required tables if they don't exist.
func initSchema(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS messages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id   TEXT    NOT NULL,
    role       TEXT    NOT NULL,
    content    TEXT    NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS fragments (
    id         TEXT    PRIMARY KEY,
    agent_id   TEXT    NOT NULL,
    content    TEXT    NOT NULL,
    source     TEXT    NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_messages_agent ON messages(agent_id, created_at);
CREATE INDEX IF NOT EXISTS idx_fragments_agent ON fragments(agent_id, created_at);
`
	_, err := db.Exec(schema)
	return err
}

// AddMessage implements Store — writes to in-memory cache and persists to SQLite.
func (s *SQLiteStore) AddMessage(role, content string) {
	s.inmem.AddMessage(role, content)

	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.db.Exec(
		"INSERT INTO messages (agent_id, role, content, created_at) VALUES (?, ?, ?, ?)",
		s.agentID, role, content, time.Now().UnixMilli(),
	)
}

// GetMessages implements Store.
func (s *SQLiteStore) GetMessages() []Message {
	return s.inmem.GetMessages()
}

// GetContext implements Store — delegates to in-memory store.
func (s *SQLiteStore) GetContext(ctx context.Context, maxTokens int) ([]Message, error) {
	return s.inmem.GetContext(ctx, maxTokens)
}

// SaveFragment implements Store — persists a fragment to SQLite.
func (s *SQLiteStore) SaveFragment(ctx context.Context, content, source string) error {
	id := fmt.Sprintf("frag-%d", time.Now().UnixNano())
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO fragments (id, agent_id, content, source, created_at) VALUES (?, ?, ?, ?, ?)",
		id, s.agentID, content, source, time.Now().UnixMilli(),
	)
	return err
}

// SearchSimilar implements Store.
// Returns keyword-matched fragments (BM25 fallback) until sqlite-vec is integrated.
func (s *SQLiteStore) SearchSimilar(ctx context.Context, query string, topK int) ([]Fragment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, content, source, created_at
		FROM   fragments
		WHERE  agent_id = ? AND content LIKE ?
		ORDER  BY created_at DESC
		LIMIT  ?`,
		s.agentID, "%"+query+"%", topK,
	)
	if err != nil {
		return nil, fmt.Errorf("search fragments: %w", err)
	}
	defer rows.Close()

	var out []Fragment
	for rows.Next() {
		var f Fragment
		var ts int64
		if err := rows.Scan(&f.ID, &f.Content, &f.Source, &ts); err != nil {
			return nil, err
		}
		f.CreatedAt = time.UnixMilli(ts)
		out = append(out, f)
	}
	return out, rows.Err()
}

// Close implements Store.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// LoadHistory loads persisted messages from SQLite into the in-memory cache.
// Call once at agent startup to restore previous session context.
func (s *SQLiteStore) LoadHistory(limit int) error {
	rows, err := s.db.Query(
		"SELECT role, content FROM messages WHERE agent_id = ? ORDER BY created_at DESC LIMIT ?",
		s.agentID, limit,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	// Collect in reverse order (DESC → reverse to get chronological).
	type pair struct{ role, content string }
	var pairs []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.role, &p.content); err != nil {
			return err
		}
		pairs = append(pairs, p)
	}
	for i := len(pairs) - 1; i >= 0; i-- {
		s.inmem.AddMessage(pairs[i].role, pairs[i].content)
	}
	return rows.Err()
}
