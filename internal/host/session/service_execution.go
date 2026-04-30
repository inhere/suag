package session

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (s *Service) registerActiveExecution(
	ctx context.Context,
	sessionID string,
	turnID string,
	runID string,
	agentID string,
) (context.Context, *activeExecution) {
	runCtx, cancel := context.WithCancel(ctx)
	execution := &activeExecution{
		sessionID: sessionID,
		turnID:    turnID,
		runID:     runID,
		agentID:   agentID,
		cancel:    cancel,
	}

	s.activeMu.Lock()
	s.activeExecutions[activeExecutionKey(sessionID, turnID, runID)] = execution
	s.activeMu.Unlock()

	return runCtx, execution
}

func (s *Service) stopActiveExecution(sessionID, turnID, reason string) (*activeExecution, bool) {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()

	for _, execution := range s.activeExecutions {
		if execution.sessionID != sessionID || execution.turnID != turnID {
			continue
		}
		execution.stopped = true
		execution.reason = strings.TrimSpace(reason)
		execution.cancel()
		return execution, true
	}

	return nil, false
}

func (s *Service) finishActiveExecution(sessionID, turnID, runID string) *activeExecution {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()

	key := activeExecutionKey(sessionID, turnID, runID)
	execution := s.activeExecutions[key]
	if execution == nil {
		return nil
	}
	delete(s.activeExecutions, key)
	return execution
}

func activeExecutionKey(sessionID, turnID, runID string) string {
	return sessionID + ":" + turnID + ":" + runID
}

func (s *Service) persistLifecycleState(
	ctx context.Context,
	sessionModel Session,
	turnID string,
	runID string,
	runState RunState,
	turnStatus TurnStatus,
	summary string,
	failureCode string,
	finishedAt time.Time,
) error {
	if s == nil || s.store == nil {
		return nil
	}

	// Keep the entity tables aligned with the emitted lifecycle events so
	// REST recovery and websocket snapshots do not disagree after reload.
	if err := s.store.UpdateRunLifecycle(ctx, runID, runState, summary, failureCode, &finishedAt); err != nil {
		return fmt.Errorf("update run lifecycle: %w", err)
	}
	if err := s.updateRunMetadata(ctx, runID, map[string]any{
		"turn_outcome": string(turnOutcomeFromRunState(runState)),
	}); err != nil {
		return fmt.Errorf("update run terminal metadata: %w", err)
	}
	if err := s.store.UpdateTurnStatus(ctx, turnID, turnStatus, &finishedAt); err != nil {
		return fmt.Errorf("update turn status: %w", err)
	}

	// Waiting is reserved for non-terminal runs that still expect another
	// operator/user action. Once a run finishes, fails, or is canceled, the
	// session itself should fall back to OPEN so list/detail views do not look
	// like the session is still blocked on unfinished work.
	sessionModel.Status = SessionOpen
	sessionModel.UpdatedAt = finishedAt
	if err := s.store.UpdateSession(ctx, sessionModel); err != nil {
		return fmt.Errorf("update session status: %w", err)
	}
	return nil
}
