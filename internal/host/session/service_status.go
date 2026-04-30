package session

import (
	"context"
	"fmt"
	"strings"
)

func (s *Service) applyRuntimeStatus(ctx context.Context, sessionModel Session, turnID, runID string, event RuntimeEvent) error {
	if s == nil || s.repo == nil {
		return nil
	}
	if strings.TrimSpace(event.EventType) != EventRunStatus {
		return nil
	}

	runState, ok := runtimeRunState(event.Payload)
	if !ok {
		return nil
	}

	switch runState {
	case RunPreparing, RunThinking, RunDispatching, RunExternal, RunSummarizing:
		if err := s.repo.UpdateRunLifecycle(ctx, runID, runState, "", "", nil); err != nil {
			return fmt.Errorf("update run runtime state: %w", err)
		}
		return nil
	case RunInputRequired, RunAuthRequired:
		if err := s.repo.UpdateRunLifecycle(ctx, runID, runState, "", "", nil); err != nil {
			return fmt.Errorf("update waiting run state: %w", err)
		}
		if err := s.repo.UpdateTurnStatus(ctx, turnID, TurnWaiting, nil); err != nil {
			return fmt.Errorf("update waiting turn status: %w", err)
		}
		sessionModel.Status = SessionWaiting
		sessionModel.UpdatedAt = s.now().UTC()
		if err := s.repo.UpdateSession(ctx, sessionModel); err != nil {
			return fmt.Errorf("update waiting session status: %w", err)
		}
		return nil
	default:
		return nil
	}
}

func runtimeRunState(payload map[string]any) (RunState, bool) {
	if len(payload) == 0 {
		return "", false
	}
	raw, _ := payload["state"].(string)
	state := RunState(strings.TrimSpace(raw))
	switch state {
	case RunPreparing, RunThinking, RunDispatching, RunExternal, RunInputRequired, RunAuthRequired, RunSummarizing:
		return state, true
	default:
		return "", false
	}
}

func isWaitingRunState(state RunState) bool {
	switch state {
	case RunExternal, RunInputRequired, RunAuthRequired:
		return true
	default:
		return false
	}
}

func (s *Service) persistExternalTask(ctx context.Context, sessionID, turnID, runID string, task *ExternalTask) error {
	if task == nil {
		return nil
	}

	taskModel := *task
	if strings.TrimSpace(taskModel.ExternalTaskID) == "" {
		taskModel.ExternalTaskID = NewExternalTaskID()
	}
	if strings.TrimSpace(taskModel.SessionID) == "" {
		taskModel.SessionID = sessionID
	}
	if strings.TrimSpace(taskModel.TurnID) == "" {
		taskModel.TurnID = turnID
	}
	if strings.TrimSpace(taskModel.RunID) == "" {
		taskModel.RunID = runID
	}
	if taskModel.Metadata == nil {
		taskModel.Metadata = map[string]any{}
	}

	return s.repo.CreateExternalTask(ctx, taskModel)
}
