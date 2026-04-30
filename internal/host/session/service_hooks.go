package session

import (
	"context"
	"strings"
)

func (s *Service) appendAndPublishEvent(ctx context.Context, event RunEvent) (RunEvent, error) {
	if s == nil || s.store == nil {
		return event, nil
	}

	storedEvent, err := s.store.AppendEvent(ctx, event)
	if err != nil {
		return RunEvent{}, err
	}
	if s.pub != nil {
		if err := s.pub.PublishSessionEvent(ctx, storedEvent); err != nil {
			return RunEvent{}, err
		}
	}
	return storedEvent, nil
}

func (s *Service) runRecapHooks(ctx context.Context, input RecapInput) {
	if s == nil || len(s.recaps) == 0 {
		return
	}

	for _, hook := range s.recaps {
		if hook == nil {
			continue
		}
		if err := hook.OnTurnRecap(ctx, input); err != nil {
			// Recap hooks are best-effort. They must never break the host main
			// lifecycle because later memory/review sinks may be optional.
			continue
		}
	}
}

func (s *Service) runPostTurnActionHooks(ctx context.Context, input PostTurnActionInput) {
	if s == nil || len(s.posts) == 0 {
		return
	}

	mergedPlannerHint := input.PlannerHint
	mergedNextActionHint := input.NextActionHint
	for _, hook := range s.posts {
		if hook == nil {
			continue
		}
		input.PlannerHint = mergedPlannerHint
		input.NextActionHint = mergedNextActionHint

		result, err := hook.OnPostTurnAction(ctx, input)
		if err != nil {
			// Post-turn follow-up is best-effort. It should never break the main
			// session lifecycle because planner/review sinks remain optional in v0.
			continue
		}
		if result == nil {
			continue
		}
		// Keep the merge rule simple and deterministic for v0: later hooks may
		// refine the lightweight hints produced by earlier hooks.
		if result.PlannerHint != nil {
			mergedPlannerHint = result.PlannerHint
		}
		if result.NextActionHint != nil {
			mergedNextActionHint = result.NextActionHint
		}
	}

	metadata := map[string]any{}
	if mergedPlannerHint != nil {
		metadata["planner_hint"] = plannerHintMetadata(mergedPlannerHint)
	}
	if mergedNextActionHint != nil {
		metadata["next_action_hint"] = nextActionHintMetadata(mergedNextActionHint)
	}
	if len(metadata) == 0 {
		return
	}
	if err := s.updateRunMetadata(ctx, input.RunID, metadata); err != nil {
		// Persisting planner-lite hints is also best-effort; the main turn is
		// already terminal and should not regress because of optional follow-up.
		return
	}
}

func plannerHintMetadata(hint *PlannerHint) map[string]any {
	if hint == nil {
		return nil
	}

	out := map[string]any{
		"stage":   strings.TrimSpace(hint.Stage),
		"summary": strings.TrimSpace(hint.Summary),
	}
	if len(hint.Metadata) > 0 {
		out["metadata"] = cloneAnyMap(hint.Metadata)
	}
	return out
}

func nextActionHintMetadata(hint *NextActionHint) map[string]any {
	if hint == nil {
		return nil
	}

	out := map[string]any{
		"kind":    strings.TrimSpace(hint.Kind),
		"title":   strings.TrimSpace(hint.Title),
		"message": strings.TrimSpace(hint.Message),
	}
	if len(hint.Metadata) > 0 {
		out["metadata"] = cloneAnyMap(hint.Metadata)
	}
	return out
}

func cloneAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}

	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}
