package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (s *SQLiteRepository) CreateSession(ctx context.Context, session Session) error {
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

func (s *SQLiteRepository) GetSession(ctx context.Context, sessionID string) (Session, error) {
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
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, fmt.Errorf("get session: %w", ErrSessionNotFound)
		}
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

func (s *SQLiteRepository) ListSessions(ctx context.Context) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT session_id, title, status, last_turn_id, created_at, updated_at, metadata_json
		FROM sessions
		ORDER BY updated_at DESC, created_at DESC, session_id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]Session, 0)
	for rows.Next() {
		var (
			session         Session
			createdAt       string
			updatedAt       string
			rawMetadataJSON []byte
		)

		if err := rows.Scan(
			&session.SessionID,
			&session.Title,
			&session.Status,
			&session.LastTurnID,
			&createdAt,
			&updatedAt,
			&rawMetadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}

		session.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		session.UpdatedAt, err = parseTime(updatedAt)
		if err != nil {
			return nil, err
		}
		session.Metadata, err = unmarshalJSONMap(rawMetadataJSON)
		if err != nil {
			return nil, err
		}

		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}

	return sessions, nil
}

func (s *SQLiteRepository) UpdateSession(ctx context.Context, session Session) error {
	if err := validateID("session", session.SessionID); err != nil {
		return err
	}
	metadata, err := marshalJSON(session.Metadata)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE sessions
		SET title = ?, status = ?, last_turn_id = ?, updated_at = ?, metadata_json = ?
		WHERE session_id = ?
	`, session.Title, session.Status, session.LastTurnID, formatTime(session.UpdatedAt), metadata, session.SessionID)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update session rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrSessionNotFound
	}
	return nil
}
