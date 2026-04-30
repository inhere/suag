package session

import (
	"context"
	"fmt"
)

func (s *SQLiteRepository) AppendEvent(ctx context.Context, event RunEvent) (RunEvent, error) {
	if err := validateID("event", event.EventID); err != nil {
		return RunEvent{}, err
	}
	payload, err := marshalJSON(event.Payload)
	if err != nil {
		return RunEvent{}, err
	}

	// Session event sequence is assigned by the store so websocket incremental
	// reads and snapshot recovery share one session-scoped ordering authority.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RunEvent{}, fmt.Errorf("begin append event tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO run_events (
			event_id, session_id, turn_id, run_id, agent_id, event_type,
			sequence, payload_json, source, created_at
		)
		VALUES (
			?, ?, ?, ?, ?, ?,
			COALESCE((SELECT MAX(sequence) + 1 FROM run_events WHERE session_id = ?), 1),
			?, ?, ?
		)
	`, event.EventID, event.SessionID, event.TurnID, event.RunID, event.AgentID, event.EventType, event.SessionID, payload, event.Source, formatTime(event.CreatedAt))
	if err != nil {
		return RunEvent{}, fmt.Errorf("append event: %w", err)
	}

	if err := tx.QueryRowContext(ctx, `
		SELECT sequence
		FROM run_events
		WHERE event_id = ?
	`, event.EventID).Scan(&event.Sequence); err != nil {
		return RunEvent{}, fmt.Errorf("query appended event sequence: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return RunEvent{}, fmt.Errorf("commit append event tx: %w", err)
	}
	return event, nil
}

func (s *SQLiteRepository) ListEventsBySession(ctx context.Context, sessionID string, afterSequence int64) ([]RunEvent, error) {
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
