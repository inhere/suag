package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/inhere/axon/pkg/agentcore"

	_ "modernc.org/sqlite"
)

type SQLiteStorage struct {
	db *sql.DB
}

func NewStorage(dbPath string) (*SQLiteStorage, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, wrapError("create sqlite storage directory", "", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, wrapError("open sqlite storage", "", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		_ = db.Close()
		return nil, wrapError("enable WAL", "", err)
	}
	if err := initSchema(db); err != nil {
		_ = db.Close()
		return nil, wrapError("initialize schema", "", err)
	}
	return &SQLiteStorage{db: db}, nil
}

func (s *SQLiteStorage) LoadRun(ctx context.Context, runID string) (*agentcore.RunState, error) {
	if err := validateRunID(runID); err != nil {
		return nil, err
	}

	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT state_json FROM agentcore_runs WHERE run_id = ?`, runID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, wrapError("load run snapshot", runID, os.ErrNotExist)
	}
	if err != nil {
		return nil, wrapError("load run snapshot", runID, err)
	}

	var state agentcore.RunState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, wrapError("decode run snapshot", runID, err)
	}
	return &state, nil
}

func (s *SQLiteStorage) LoadMessages(ctx context.Context, runID string) ([]agentcore.Message, error) {
	if err := validateRunID(runID); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT message_json
		FROM agentcore_messages
		WHERE run_id = ?
		ORDER BY seq ASC
	`, runID)
	if err != nil {
		return nil, wrapError("load messages log", runID, err)
	}
	defer rows.Close()

	messages := make([]agentcore.Message, 0)
	lineNo := 0
	for rows.Next() {
		lineNo++
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, wrapError("read messages log", runID, err)
		}
		var msg agentcore.Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			return nil, wrapError("decode messages log", runID, fmt.Errorf("line %d: %w", lineNo, err))
		}
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapError("read messages log", runID, err)
	}
	if lineNo == 0 {
		return nil, wrapError("load messages log", runID, os.ErrNotExist)
	}
	return messages, nil
}

func (s *SQLiteStorage) SaveRun(ctx context.Context, state *agentcore.RunState) error {
	if state == nil {
		return wrapError("save run snapshot", "", errors.New("run state is nil"))
	}
	if err := validateRunID(state.RunID); err != nil {
		return err
	}

	raw, err := json.Marshal(state)
	if err != nil {
		return wrapError("encode run snapshot", state.RunID, err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO agentcore_runs (run_id, state_json)
		VALUES (?, ?)
		ON CONFLICT(run_id) DO UPDATE SET state_json = excluded.state_json
	`, state.RunID, raw)
	if err != nil {
		return wrapError("save run snapshot", state.RunID, err)
	}
	return nil
}

func (s *SQLiteStorage) AppendMessages(ctx context.Context, runID string, msgs ...agentcore.Message) error {
	if err := validateRunID(runID); err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapError("begin messages append", runID, err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var nextSeq int64
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM agentcore_messages WHERE run_id = ?`, runID).Scan(&nextSeq)
	if err != nil {
		return wrapError("load message sequence", runID, err)
	}

	for _, msg := range msgs {
		nextSeq++
		raw, err := json.Marshal(msg)
		if err != nil {
			return wrapError("encode messages log", runID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO agentcore_messages (run_id, seq, message_json)
			VALUES (?, ?, ?)
		`, runID, nextSeq, raw); err != nil {
			return wrapError("append messages log", runID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return wrapError("commit messages log", runID, err)
	}
	return nil
}

func (s *SQLiteStorage) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func initSchema(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS agentcore_runs (
    run_id     TEXT PRIMARY KEY,
    state_json BLOB NOT NULL
);

CREATE TABLE IF NOT EXISTS agentcore_messages (
    run_id       TEXT NOT NULL,
    seq          INTEGER NOT NULL,
    message_json BLOB NOT NULL,
    PRIMARY KEY (run_id, seq)
);
`
	_, err := db.Exec(schema)
	return err
}

func validateRunID(runID string) error {
	if strings.TrimSpace(runID) == "" {
		return wrapError("validate run id", runID, errors.New("run id is required"))
	}
	if filepath.IsAbs(runID) || filepath.VolumeName(runID) != "" {
		return wrapError("validate run id", runID, errors.New("run id must not be an absolute path"))
	}
	if runID == "." || runID == ".." {
		return wrapError("validate run id", runID, errors.New("run id must not reference the current or parent directory"))
	}
	if strings.ContainsAny(runID, `/\`) {
		return wrapError("validate run id", runID, errors.New("run id must not contain path separators"))
	}
	if cleaned := filepath.Clean(runID); cleaned != runID || cleaned == "." || cleaned == ".." {
		return wrapError("validate run id", runID, errors.New("run id must resolve to a single safe path segment"))
	}
	return nil
}

func wrapError(action, runID string, err error) error {
	if err == nil {
		return nil
	}
	if runID == "" {
		return &agentcore.StorageError{Err: fmt.Errorf("%s: %w", action, err)}
	}
	return &agentcore.StorageError{Err: fmt.Errorf("%s for run %q: %w", action, runID, err)}
}
