package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}

	// Schema ownership is delegated to miglite SQL migrations under db/migrations.
	// Store startup only verifies the expected tables are already present.
	if err := ensureRequiredTables(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) CreateSession(ctx context.Context, session Session) error {
	if err := validateID("session", session.SessionID); err != nil {
		return err
	}
	metadata, err := marshalJSON(session.Metadata)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO sessions (session_id, title, status, last_turn_id, created_at, updated_at, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, session.SessionID, session.Title, session.Status, session.LastTurnID, formatTime(session.CreatedAt), formatTime(session.UpdatedAt), metadata)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetSession(ctx context.Context, sessionID string) (Session, error) {
	if err := validateID("session", sessionID); err != nil {
		return Session{}, err
	}

	var (
		session         Session
		createdAt       string
		updatedAt       string
		rawMetadataJSON []byte
	)

	err := s.db.QueryRowContext(ctx, `
		SELECT session_id, title, status, last_turn_id, created_at, updated_at, metadata_json
		FROM sessions
		WHERE session_id = ?
	`, sessionID).Scan(
		&session.SessionID,
		&session.Title,
		&session.Status,
		&session.LastTurnID,
		&createdAt,
		&updatedAt,
		&rawMetadataJSON,
	)
	if err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}

	session.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return Session{}, err
	}
	session.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Session{}, err
	}
	session.Metadata, err = unmarshalJSONMap(rawMetadataJSON)
	if err != nil {
		return Session{}, err
	}

	return session, nil
}

func (s *SQLiteStore) CreateTurn(ctx context.Context, turn Turn) error {
	if err := validateID("turn", turn.TurnID); err != nil {
		return err
	}
	metadata, err := marshalJSON(turn.Metadata)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO turns (
			turn_id, session_id, initiator_type, initiator_id, input_message_id,
			input_summary, status, primary_run_id, created_at, completed_at, metadata_json
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, turn.TurnID, turn.SessionID, turn.InitiatorType, turn.InitiatorID, turn.InputMessageID, turn.InputSummary, turn.Status, turn.PrimaryRunID, formatTime(turn.CreatedAt), formatTime(turn.CompletedAt), metadata)
	if err != nil {
		return fmt.Errorf("create turn: %w", err)
	}
	return nil
}

func (s *SQLiteStore) CreateRun(ctx context.Context, run Run) error {
	if err := validateID("run", run.RunID); err != nil {
		return err
	}
	metadata, err := marshalJSON(run.Metadata)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO runs (
			run_id, session_id, turn_id, agent_id, role, parent_run_id,
			resume_from_run_id, state, started_at, ended_at, failure_code, summary, metadata_json
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, run.RunID, run.SessionID, run.TurnID, run.AgentID, run.Role, run.ParentRunID, run.ResumeFromRunID, run.State, formatTime(run.StartedAt), formatTime(run.EndedAt), run.FailureCode, run.Summary, metadata)
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateRunState(ctx context.Context, runID string, state RunState, endedAt *time.Time) error {
	if err := validateID("run", runID); err != nil {
		return err
	}

	ended := ""
	if endedAt != nil {
		ended = formatTime(*endedAt)
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE runs
		SET state = ?, ended_at = ?
		WHERE run_id = ?
	`, state, ended, runID)
	if err != nil {
		return fmt.Errorf("update run state: %w", err)
	}
	return nil
}

func (s *SQLiteStore) AppendEvent(ctx context.Context, event RunEvent) error {
	if err := validateID("event", event.EventID); err != nil {
		return err
	}
	payload, err := marshalJSON(event.Payload)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO run_events (
			event_id, session_id, turn_id, run_id, agent_id, event_type,
			sequence, payload_json, source, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, event.EventID, event.SessionID, event.TurnID, event.RunID, event.AgentID, event.EventType, event.Sequence, payload, event.Source, formatTime(event.CreatedAt))
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListEventsBySession(ctx context.Context, sessionID string, afterSequence int64) ([]RunEvent, error) {
	if err := validateID("session", sessionID); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT event_id, session_id, turn_id, run_id, agent_id, event_type,
		       sequence, payload_json, source, created_at
		FROM run_events
		WHERE session_id = ? AND sequence > ?
		ORDER BY sequence ASC
	`, sessionID, afterSequence)
	if err != nil {
		return nil, fmt.Errorf("list session events: %w", err)
	}
	defer rows.Close()

	var events []RunEvent
	for rows.Next() {
		var (
			event      RunEvent
			rawPayload []byte
			createdAt  string
		)
		if err := rows.Scan(
			&event.EventID,
			&event.SessionID,
			&event.TurnID,
			&event.RunID,
			&event.AgentID,
			&event.EventType,
			&event.Sequence,
			&rawPayload,
			&event.Source,
			&createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan session event: %w", err)
		}
		event.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		event.Payload, err = unmarshalJSONMap(rawPayload)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session events: %w", err)
	}
	return events, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func validateID(kind, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s id is required", kind)
	}
	return nil
}

func ensureRequiredTables(db *sql.DB) error {
	requiredTables := []string{
		"sessions",
		"turns",
		"runs",
		"run_events",
		"external_tasks",
		"recaps",
	}

	missing := make([]string, 0, len(requiredTables))
	for _, tableName := range requiredTables {
		exists, err := tableExists(db, tableName)
		if err != nil {
			return err
		}
		if !exists {
			missing = append(missing, tableName)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required tables; run migrations under db/migrations first: %s", strings.Join(missing, ", "))
	}
	return nil
}

func tableExists(db *sql.DB, tableName string) (bool, error) {
	var exists int
	err := db.QueryRow(`
		SELECT COUNT(1)
		FROM sqlite_master
		WHERE type = 'table' AND name = ?
	`, tableName).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check table %s exists: %w", tableName, err)
	}
	return exists > 0, nil
}

func marshalJSON(value map[string]any) ([]byte, error) {
	if value == nil {
		value = map[string]any{}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal json: %w", err)
	}
	return data, nil
}

func unmarshalJSONMap(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("unmarshal json: %w", err)
	}
	if out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}

func formatTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func parseTime(raw string) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, nil
	}
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time: %w", err)
	}
	return ts, nil
}
