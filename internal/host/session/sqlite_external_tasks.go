package session

import (
	"context"
	"fmt"
	"strings"
)

func (s *SQLiteStore) CreateExternalTask(ctx context.Context, task ExternalTask) error {
	if err := validateID("external task", task.ExternalTaskID); err != nil {
		return err
	}
	metadata, err := marshalJSON(task.Metadata)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO external_tasks (
			external_task_id, session_id, turn_id, run_id, adapter,
			remote_agent_name, remote_session_id, remote_task_id, state,
			last_heartbeat_at, result_summary, metadata_json
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, task.ExternalTaskID, task.SessionID, task.TurnID, task.RunID, task.Adapter, task.RemoteAgentName, task.RemoteSessionID, task.RemoteTaskID, task.State, formatTime(task.LastHeartbeatAt), task.ResultSummary, metadata)
	if err != nil {
		return fmt.Errorf("create external task: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateExternalTask(ctx context.Context, task ExternalTask) error {
	if err := validateID("external task", task.ExternalTaskID); err != nil {
		return err
	}
	metadata, err := marshalJSON(task.Metadata)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE external_tasks
		SET adapter = ?, remote_agent_name = ?, remote_session_id = ?, remote_task_id = ?,
		    state = ?, last_heartbeat_at = ?, result_summary = ?, metadata_json = ?
		WHERE external_task_id = ?
	`, task.Adapter, task.RemoteAgentName, task.RemoteSessionID, task.RemoteTaskID, task.State, formatTime(task.LastHeartbeatAt), task.ResultSummary, metadata, task.ExternalTaskID)
	if err != nil {
		return fmt.Errorf("update external task: %w", err)
	}
	if _, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("update external task rows affected: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListExternalTasksByRun(ctx context.Context, runID string) ([]ExternalTask, error) {
	if err := validateID("run", runID); err != nil {
		return nil, err
	}

	return s.listExternalTasks(ctx, `
		SELECT external_task_id, session_id, turn_id, run_id, adapter,
		       remote_agent_name, remote_session_id, remote_task_id, state,
		       last_heartbeat_at, result_summary, metadata_json
		FROM external_tasks
		WHERE run_id = ?
		ORDER BY external_task_id ASC
	`, runID, "list external tasks by run")
}

func (s *SQLiteStore) ListExternalTasksBySession(ctx context.Context, sessionID string) ([]ExternalTask, error) {
	if err := validateID("session", sessionID); err != nil {
		return nil, err
	}

	return s.listExternalTasks(ctx, `
		SELECT external_task_id, session_id, turn_id, run_id, adapter,
		       remote_agent_name, remote_session_id, remote_task_id, state,
		       last_heartbeat_at, result_summary, metadata_json
		FROM external_tasks
		WHERE session_id = ?
		ORDER BY external_task_id ASC
	`, sessionID, "list external tasks by session")
}

func (s *SQLiteStore) listExternalTasks(ctx context.Context, query string, arg string, op string) ([]ExternalTask, error) {
	rows, err := s.db.QueryContext(ctx, `
	`+strings.TrimSpace(query), arg)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer rows.Close()

	var tasks []ExternalTask
	for rows.Next() {
		var (
			task            ExternalTask
			lastHeartbeatAt string
			rawMetadataJSON []byte
		)
		if err := rows.Scan(
			&task.ExternalTaskID,
			&task.SessionID,
			&task.TurnID,
			&task.RunID,
			&task.Adapter,
			&task.RemoteAgentName,
			&task.RemoteSessionID,
			&task.RemoteTaskID,
			&task.State,
			&lastHeartbeatAt,
			&task.ResultSummary,
			&rawMetadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan external task: %w", err)
		}
		task.LastHeartbeatAt, err = parseTime(lastHeartbeatAt)
		if err != nil {
			return nil, err
		}
		task.Metadata, err = unmarshalJSONMap(rawMetadataJSON)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate external tasks: %w", err)
	}
	return tasks, nil
}
