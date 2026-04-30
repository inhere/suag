package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

func (s *Service) ensureSession(ctx context.Context, sessionID string, now time.Time, createIfMissing bool) (Session, error) {
	sessionModel, err := s.repo.GetSession(ctx, sessionID)
	if err == nil {
		return sessionModel, nil
	}
	if !errors.Is(err, ErrSessionNotFound) {
		return Session{}, err
	}
	if !createIfMissing {
		return Session{}, ErrSessionNotFound
	}

	sessionModel = Session{
		SessionID: sessionID,
		Title:     "Session " + sessionID,
		Status:    SessionOpen,
		CreatedAt: now,
		UpdatedAt: now,
		Metadata:  map[string]any{},
	}
	if err := s.repo.CreateSession(ctx, sessionModel); err != nil {
		return Session{}, fmt.Errorf("create missing session: %w", err)
	}
	return sessionModel, nil
}

func (s *Service) validateContinueTurnState(ctx context.Context, sessionID string) error {
	waiting, err := s.latestContinuableRun(ctx, sessionID)
	if err != nil {
		return err
	}
	return validateContinueRunState(waiting.state)
}

func (s *Service) latestRunIDForTurn(ctx context.Context, sessionID, turnID string) (string, error) {
	if s == nil || s.repo == nil {
		return "", nil
	}
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(turnID) == "" {
		return "", nil
	}

	events, err := s.repo.ListEventsBySession(ctx, sessionID, 0)
	if err != nil {
		return "", err
	}

	for i := len(events) - 1; i >= 0; i-- {
		if events[i].TurnID != turnID {
			continue
		}
		if strings.TrimSpace(events[i].RunID) == "" {
			continue
		}
		return events[i].RunID, nil
	}
	return "", nil
}

type waitingRunRef struct {
	turnID string
	runID  string
	state  RunState
}

func (s *Service) latestContinuableRun(ctx context.Context, sessionID string) (waitingRunRef, error) {
	if s == nil || s.repo == nil {
		return waitingRunRef{}, nil
	}

	sessionModel, err := s.repo.GetSession(ctx, sessionID)
	if err != nil {
		return waitingRunRef{}, err
	}

	turnID := strings.TrimSpace(sessionModel.LastTurnID)
	if turnID == "" {
		return waitingRunRef{}, fmt.Errorf("session is not waiting for continue_turn")
	}

	runID, err := s.latestRunIDForTurn(ctx, sessionID, turnID)
	if err != nil {
		return waitingRunRef{}, err
	}
	if runID == "" {
		return waitingRunRef{}, fmt.Errorf("latest waiting run is unavailable; start a new turn or reissue the task")
	}

	runModel, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return waitingRunRef{}, fmt.Errorf("latest waiting run is unavailable; start a new turn or reissue the task")
		}
		return waitingRunRef{}, fmt.Errorf("load latest run: %w", err)
	}

	return waitingRunRef{
		turnID: turnID,
		runID:  runID,
		state:  runModel.State,
	}, nil
}

func validateContinueRunState(state RunState) error {
	// continue_turn is only valid for the latest waiting run that is expecting
	// free-form human input. Approval waits must go through HITL so we do not
	// mix operator approval with normal conversational continuation.
	switch state {
	case RunInputRequired:
		return nil
	case RunAuthRequired:
		return fmt.Errorf("session is waiting for approval; respond via HITL instead of continue_turn")
	default:
		return fmt.Errorf("session is not waiting for continue_turn")
	}
}

func validateStartTurnInput(in StartTurnInput) error {
	return validateTurnRequest(in.SessionID, in.AgentID, in.UserMessage, in.InitiatorType)
}

func validateContinueTurnInput(in ContinueTurnInput) error {
	return validateTurnRequest(in.SessionID, in.AgentID, in.Message, in.InitiatorType)
}

func validateStopTurnInput(in StopTurnInput) error {
	if strings.TrimSpace(in.SessionID) == "" {
		return fmt.Errorf("session id is required")
	}
	if strings.TrimSpace(in.TurnID) == "" {
		return fmt.Errorf("turn id is required")
	}
	return nil
}

func validateUpdateMemoryCandidateInput(in UpdateMemoryCandidateInput) error {
	if strings.TrimSpace(in.CandidateID) == "" {
		return fmt.Errorf("candidate id is required")
	}
	switch in.Status {
	case MemoryCandidateApplied, MemoryCandidateRejected:
		return nil
	default:
		return fmt.Errorf("memory candidate status must be applied or rejected")
	}
}

func validateTurnRequest(sessionID, agentID, message string, initiatorType InitiatorType) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session id is required")
	}
	if strings.TrimSpace(agentID) == "" {
		return fmt.Errorf("agent id is required")
	}
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("user message is required")
	}
	if strings.TrimSpace(string(initiatorType)) == "" {
		return fmt.Errorf("initiator type is required")
	}
	return nil
}

func (s *Service) initialRunMetadata(createIfMissing bool) map[string]any {
	metadata := map[string]any{}
	if createIfMissing {
		metadata["loop_phase"] = string(LoopPhaseInitial)
		metadata["continue_mode"] = string(ContinueModeStart)
		return metadata
	}
	metadata["loop_phase"] = string(LoopPhaseResumed)
	metadata["resume_source"] = "continue_turn"
	metadata["continue_mode"] = string(ContinueModeMessage)
	return metadata
}

func (s *Service) updateRunMetadata(ctx context.Context, runID string, extra map[string]any) error {
	if s == nil || s.repo == nil || len(extra) == 0 {
		return nil
	}

	runModel, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if runModel.Metadata == nil {
		runModel.Metadata = map[string]any{}
	}
	for key, value := range extra {
		runModel.Metadata[key] = value
	}
	return s.repo.UpdateRunMetadata(ctx, runID, runModel.Metadata)
}

func (s *Service) loadRunMetadata(ctx context.Context, runID string) map[string]any {
	if s == nil || s.repo == nil || strings.TrimSpace(runID) == "" {
		return nil
	}

	runModel, err := s.repo.GetRun(ctx, runID)
	if err != nil || len(runModel.Metadata) == 0 {
		return nil
	}

	out := make(map[string]any, len(runModel.Metadata))
	for key, value := range runModel.Metadata {
		out[key] = value
	}
	return out
}

func turnOutcomeFromRunState(state RunState) TurnOutcome {
	switch state {
	case RunCompleted:
		return TurnOutcomeCompleted
	case RunFailed:
		return TurnOutcomeFailed
	case RunCanceled:
		return TurnOutcomeCanceled
	default:
		return ""
	}
}
