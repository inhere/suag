package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
)

func (s *SQLiteRepository) CreateRecap(ctx context.Context, recap Recap) error {
	if err := validateID("recap", recap.RecapID); err != nil {
		return err
	}
	if err := validateID("session", recap.SessionID); err != nil {
		return err
	}
	if err := validateID("turn", recap.TurnID); err != nil {
		return err
	}
	metadata, err := marshalJSON(recap.Metadata)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO recaps (recap_id, session_id, turn_id, run_id, kind, target, content, created_at, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, recap.RecapID, recap.SessionID, recap.TurnID, recap.RunID, recap.Kind, recap.Target, recap.Content, formatTime(recap.CreatedAt), metadata)
	if err != nil {
		return fmt.Errorf("create recap: %w", err)
	}
	return nil
}

func (s *SQLiteRepository) ListRecapsByTurn(ctx context.Context, sessionID string, turnID string) ([]Recap, error) {
	if err := validateID("session", sessionID); err != nil {
		return nil, err
	}
	if err := validateID("turn", turnID); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT recap_id, session_id, turn_id, run_id, kind, target, content, created_at, metadata_json
		FROM recaps
		WHERE session_id = ? AND turn_id = ?
		ORDER BY created_at ASC, recap_id ASC
	`, sessionID, turnID)
	if err != nil {
		return nil, fmt.Errorf("list recaps by turn: %w", err)
	}
	defer rows.Close()

	recaps := make([]Recap, 0)
	for rows.Next() {
		var (
			recap           Recap
			createdAt       string
			rawMetadataJSON []byte
		)
		if err := rows.Scan(
			&recap.RecapID,
			&recap.SessionID,
			&recap.TurnID,
			&recap.RunID,
			&recap.Kind,
			&recap.Target,
			&recap.Content,
			&createdAt,
			&rawMetadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan recap: %w", err)
		}
		recap.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		recap.Metadata, err = unmarshalJSONMap(rawMetadataJSON)
		if err != nil {
			return nil, err
		}
		recaps = append(recaps, recap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recaps: %w", err)
	}
	return recaps, nil
}

func (s *SQLiteRepository) CreateMemoryCandidate(ctx context.Context, candidate MemoryCandidate) error {
	if err := validateID("memory candidate", candidate.CandidateID); err != nil {
		return err
	}
	if err := validateID("session", candidate.SessionID); err != nil {
		return err
	}
	if err := validateID("turn", candidate.TurnID); err != nil {
		return err
	}
	metadata, err := marshalJSON(candidate.Metadata)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO memory_candidates (
			candidate_id, session_id, turn_id, run_id, source_recap_id,
			target, kind, content, score, status, created_at, metadata_json
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, candidate.CandidateID, candidate.SessionID, candidate.TurnID, candidate.RunID, candidate.SourceRecapID, candidate.Target, candidate.Kind, candidate.Content, candidate.Score, candidate.Status, formatTime(candidate.CreatedAt), metadata)
	if err != nil {
		return fmt.Errorf("create memory candidate: %w", err)
	}
	return nil
}

func (s *SQLiteRepository) GetMemoryCandidate(ctx context.Context, candidateID string) (MemoryCandidate, error) {
	if err := validateID("memory candidate", candidateID); err != nil {
		return MemoryCandidate{}, err
	}

	var (
		candidate       MemoryCandidate
		createdAt       string
		rawMetadataJSON []byte
	)

	err := s.db.QueryRowContext(ctx, `
		SELECT candidate_id, session_id, turn_id, run_id, source_recap_id,
		       target, kind, content, score, status, created_at, metadata_json
		FROM memory_candidates
		WHERE candidate_id = ?
	`, candidateID).Scan(
		&candidate.CandidateID,
		&candidate.SessionID,
		&candidate.TurnID,
		&candidate.RunID,
		&candidate.SourceRecapID,
		&candidate.Target,
		&candidate.Kind,
		&candidate.Content,
		&candidate.Score,
		&candidate.Status,
		&createdAt,
		&rawMetadataJSON,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MemoryCandidate{}, fmt.Errorf("get memory candidate: %w", os.ErrNotExist)
		}
		return MemoryCandidate{}, fmt.Errorf("get memory candidate: %w", err)
	}

	candidate.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return MemoryCandidate{}, err
	}
	candidate.Metadata, err = unmarshalJSONMap(rawMetadataJSON)
	if err != nil {
		return MemoryCandidate{}, err
	}
	return candidate, nil
}

func (s *SQLiteRepository) ListMemoryCandidatesByTurn(ctx context.Context, sessionID string, turnID string) ([]MemoryCandidate, error) {
	if err := validateID("session", sessionID); err != nil {
		return nil, err
	}
	if err := validateID("turn", turnID); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT candidate_id, session_id, turn_id, run_id, source_recap_id,
		       target, kind, content, score, status, created_at, metadata_json
		FROM memory_candidates
		WHERE session_id = ? AND turn_id = ?
		ORDER BY created_at ASC, candidate_id ASC
	`, sessionID, turnID)
	if err != nil {
		return nil, fmt.Errorf("list memory candidates by turn: %w", err)
	}
	defer rows.Close()

	candidates := make([]MemoryCandidate, 0)
	for rows.Next() {
		var (
			candidate       MemoryCandidate
			createdAt       string
			rawMetadataJSON []byte
		)
		if err := rows.Scan(
			&candidate.CandidateID,
			&candidate.SessionID,
			&candidate.TurnID,
			&candidate.RunID,
			&candidate.SourceRecapID,
			&candidate.Target,
			&candidate.Kind,
			&candidate.Content,
			&candidate.Score,
			&candidate.Status,
			&createdAt,
			&rawMetadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan memory candidate: %w", err)
		}
		candidate.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		candidate.Metadata, err = unmarshalJSONMap(rawMetadataJSON)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memory candidates: %w", err)
	}
	return candidates, nil
}

func (s *SQLiteRepository) ListMemoryCandidatesBySession(ctx context.Context, sessionID string) ([]MemoryCandidate, error) {
	if err := validateID("session", sessionID); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT candidate_id, session_id, turn_id, run_id, source_recap_id,
		       target, kind, content, score, status, created_at, metadata_json
		FROM memory_candidates
		WHERE session_id = ?
		ORDER BY created_at ASC, candidate_id ASC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list memory candidates by session: %w", err)
	}
	defer rows.Close()

	candidates := make([]MemoryCandidate, 0)
	for rows.Next() {
		var (
			candidate       MemoryCandidate
			createdAt       string
			rawMetadataJSON []byte
		)
		if err := rows.Scan(
			&candidate.CandidateID,
			&candidate.SessionID,
			&candidate.TurnID,
			&candidate.RunID,
			&candidate.SourceRecapID,
			&candidate.Target,
			&candidate.Kind,
			&candidate.Content,
			&candidate.Score,
			&candidate.Status,
			&createdAt,
			&rawMetadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan memory candidate: %w", err)
		}
		candidate.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		candidate.Metadata, err = unmarshalJSONMap(rawMetadataJSON)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memory candidates: %w", err)
	}
	return candidates, nil
}

func (s *SQLiteRepository) UpdateMemoryCandidateStatus(ctx context.Context, candidateID string, status MemoryCandidateStatus) error {
	if err := validateID("memory candidate", candidateID); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE memory_candidates
		SET status = ?
		WHERE candidate_id = ?
	`, strings.TrimSpace(string(status)), candidateID)
	if err != nil {
		return fmt.Errorf("update memory candidate status: %w", err)
	}
	return nil
}
