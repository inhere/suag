package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

func (s *SQLiteRepository) CreateRun(ctx context.Context, run Run) error {
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

func (s *SQLiteRepository) UpdateRunLifecycle(ctx context.Context, runID string, state RunState, summary string, failureCode string, endedAt *time.Time) error {
	if err := validateID("run", runID); err != nil {
		return err
	}

	ended := ""
	if endedAt != nil {
		ended = formatTime(*endedAt)
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE runs
		SET state = ?, summary = ?, failure_code = ?, ended_at = ?
		WHERE run_id = ?
	`, state, summary, failureCode, ended, runID)
	if err != nil {
		return fmt.Errorf("update run lifecycle: %w", err)
	}
	return nil
}

func (s *SQLiteRepository) UpdateRunMetadata(ctx context.Context, runID string, metadata map[string]any) error {
	if err := validateID("run", runID); err != nil {
		return err
	}
	raw, err := marshalJSON(metadata)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE runs
		SET metadata_json = ?
		WHERE run_id = ?
	`, raw, runID)
	if err != nil {
		return fmt.Errorf("update run metadata: %w", err)
	}
	return nil
}

func (s *SQLiteRepository) GetRun(ctx context.Context, runID string) (Run, error) {
	if err := validateID("run", runID); err != nil {
		return Run{}, err
	}

	var (
		run             Run
		startedAt       string
		endedAt         string
		rawMetadataJSON []byte
	)

	err := s.db.QueryRowContext(ctx, `
		SELECT run_id, session_id, turn_id, agent_id, role, parent_run_id,
		       resume_from_run_id, state, started_at, ended_at, failure_code, summary, metadata_json
		FROM runs
		WHERE run_id = ?
	`, runID).Scan(
		&run.RunID,
		&run.SessionID,
		&run.TurnID,
		&run.AgentID,
		&run.Role,
		&run.ParentRunID,
		&run.ResumeFromRunID,
		&run.State,
		&startedAt,
		&endedAt,
		&run.FailureCode,
		&run.Summary,
		&rawMetadataJSON,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Run{}, fmt.Errorf("get run: %w", os.ErrNotExist)
		}
		return Run{}, fmt.Errorf("get run: %w", err)
	}

	run.StartedAt, err = parseTime(startedAt)
	if err != nil {
		return Run{}, err
	}
	if strings.TrimSpace(endedAt) != "" {
		run.EndedAt, err = parseTime(endedAt)
		if err != nil {
			return Run{}, err
		}
	}
	run.Metadata, err = unmarshalJSONMap(rawMetadataJSON)
	if err != nil {
		return Run{}, err
	}

	return run, nil
}
