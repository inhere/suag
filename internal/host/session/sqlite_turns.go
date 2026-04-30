package session

import (
	"context"
	"fmt"
	"time"
)

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

func (s *SQLiteStore) UpdateTurnStatus(ctx context.Context, turnID string, status TurnStatus, completedAt *time.Time) error {
	if err := validateID("turn", turnID); err != nil {
		return err
	}

	completed := ""
	if completedAt != nil {
		completed = formatTime(*completedAt)
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE turns
		SET status = ?, completed_at = ?
		WHERE turn_id = ?
	`, status, completed, turnID)
	if err != nil {
		return fmt.Errorf("update turn status: %w", err)
	}
	return nil
}
