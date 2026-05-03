package session

import (
	"context"
	"strings"
)

func (s *Service) buildRuntimeContext(ctx context.Context, sessionID, agentID, message, previousTurnID, resumeFromRunID, externalTaskID string, isContinue bool) map[string]any {
	ctxMap := map[string]any{}
	previousNextTurnPlan := s.loadPreviousNextTurnPlan(ctx, sessionID, previousTurnID)
	if len(previousNextTurnPlan) > 0 {
		ctxMap["previous_next_turn_plan"] = cloneAnyMap(previousNextTurnPlan)
	}
	if strings.TrimSpace(externalTaskID) != "" {
		ctxMap["external_task_id"] = externalTaskID
	}
	if isContinue {
		if strings.TrimSpace(previousTurnID) != "" {
			ctxMap["resume_turn_id"] = previousTurnID
		}
		if strings.TrimSpace(resumeFromRunID) != "" {
			ctxMap["resume_from_run_id"] = resumeFromRunID
		}
	}
	if s == nil {
		return ctxMap
	}
	if built := s.buildCapabilityRuntimeContext(ctx, sessionID, agentID, message, isContinue, previousNextTurnPlan); len(built) > 0 {
		ctxMap = mergeRuntimePayload(ctxMap, built)
	}
	if s.repo == nil {
		return ctxMap
	}
	if strings.TrimSpace(previousTurnID) == "" {
		return ctxMap
	}

	tasks, err := s.repo.ListExternalTasksBySession(ctx, sessionID)
	if err != nil {
		return ctxMap
	}

	resumeTasks := make([]ExternalTask, 0, len(tasks))
	for _, task := range tasks {
		if task.TurnID != previousTurnID {
			continue
		}
		if strings.TrimSpace(task.RemoteSessionID) == "" {
			continue
		}
		resumeTasks = append(resumeTasks, task)
	}
	if len(resumeTasks) == 0 {
		return ctxMap
	}

	ctxMap["resume_turn_id"] = previousTurnID
	ctxMap["resume_external_tasks"] = resumeTasks
	return ctxMap
}

func (s *Service) buildInitialSessionMetadata(sessionID, agentID, message string) map[string]any {
	if s == nil || s.sessionMetadataBuilder == nil {
		return nil
	}
	return s.sessionMetadataBuilder(SessionMetadataBuildInput{
		SessionID: sessionID,
		AgentID:   agentID,
		Message:   message,
	})
}

func (s *Service) buildCapabilityRuntimeContext(ctx context.Context, sessionID, agentID, message string, isContinue bool, previousNextTurnPlan map[string]any) map[string]any {
	if s == nil || s.runtimeContextBuilder == nil || s.repo == nil {
		return nil
	}

	sessionModel, err := s.repo.GetSession(ctx, sessionID)
	if err != nil {
		return nil
	}

	return s.runtimeContextBuilder(RuntimeContextBuildInput{
		SessionID:            sessionID,
		AgentID:              agentID,
		Message:              message,
		IsContinue:           isContinue,
		SessionMetadata:      cloneAnyMap(sessionModel.Metadata),
		PreviousNextTurnPlan: cloneAnyMap(previousNextTurnPlan),
	})
}

func (s *Service) buildContinueRuntimeContext(ctx context.Context, sessionID, agentID, message string) (map[string]any, error) {
	waiting, err := s.latestContinuableRun(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return s.buildRuntimeContext(ctx, sessionID, agentID, message, waiting.turnID, waiting.runID, "", true), nil
}

func (s *Service) loadPreviousNextTurnPlan(ctx context.Context, sessionID, previousTurnID string) map[string]any {
	if s == nil || s.repo == nil || strings.TrimSpace(previousTurnID) == "" {
		return nil
	}
	runID, err := s.latestRunIDForTurn(ctx, sessionID, previousTurnID)
	if err != nil || strings.TrimSpace(runID) == "" {
		return nil
	}

	metadata := s.loadRunMetadata(ctx, runID)
	plannerHint, ok := metadata["planner_hint"].(map[string]any)
	if !ok || len(plannerHint) == 0 {
		return nil
	}
	hintMetadata, ok := plannerHint["metadata"].(map[string]any)
	if !ok || len(hintMetadata) == 0 {
		return nil
	}
	nextTurnPlan, ok := hintMetadata["next_turn_plan"].(map[string]any)
	if !ok || len(nextTurnPlan) == 0 {
		return nil
	}
	return cloneAnyMap(nextTurnPlan)
}

func mergeRuntimePayload(payload map[string]any, extra map[string]any) map[string]any {
	if payload == nil && extra == nil {
		return map[string]any{}
	}
	out := map[string]any{}
	for key, value := range payload {
		out[key] = value
	}
	for key, value := range extra {
		if _, ok := out[key]; ok {
			continue
		}
		out[key] = value
	}
	return out
}

func withExternalTaskID(payload map[string]any, externalTaskID string) map[string]any {
	if strings.TrimSpace(externalTaskID) == "" {
		return payload
	}
	return mergeRuntimePayload(payload, map[string]any{
		"externalTaskId": strings.TrimSpace(externalTaskID),
	})
}

func isTerminalRuntimeEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case EventRunFinished, EventRunFailed, EventRunCanceled:
		return true
	default:
		return false
	}
}
