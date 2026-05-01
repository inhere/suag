package session

import (
	"context"
	"strings"
)

// CapabilityPlannerHook derives lightweight planner hints from the resolved
// capability snapshot of the just-finished turn.
type CapabilityPlannerHook struct{}

func NewCapabilityPlannerHook() *CapabilityPlannerHook {
	return &CapabilityPlannerHook{}
}

func (h *CapabilityPlannerHook) OnPostTurnAction(_ context.Context, input PostTurnActionInput) (*PostTurnActionResult, error) {
	if h == nil || input.Outcome == "" {
		return nil, nil
	}

	resolvedToolMode := decodeStringAny(input.RuntimeContext["resolved_tool_mode"])
	resolvedSkillNames := decodeStringSliceAny(input.RuntimeContext["resolved_skill_names"])
	resourceHintCount := len(decodeMapSliceAny(input.RuntimeContext["resolved_resource_hints"]))
	recallHit := decodeBoolAny(input.RuntimeContext["memory_recall_hit"])
	resolvedToolNames := decodeStringSliceAny(input.RuntimeContext["resolved_tool_names"])
	resolvedSkillMatches := decodeMapSliceAny(input.RuntimeContext["resolved_skill_matches"])

	metadata := map[string]any{
		"resolved_tool_mode":   resolvedToolMode,
		"resolved_skill_names": resolvedSkillNames,
		"resource_hint_count":  resourceHintCount,
		"recall_hit":           recallHit,
		"next_turn_plan": nextTurnPlanMetadata(
			resolvedToolMode,
			resolvedToolNames,
			resolvedSkillNames,
			resourceHintCount,
			recallHit,
		),
	}
	if len(resolvedToolNames) > 0 {
		metadata["resolved_tool_names"] = resolvedToolNames
	}
	if len(resolvedSkillMatches) > 0 {
		metadata["resolved_skill_matches"] = resolvedSkillMatches
	}

	stage := "capability_review"
	summary := plannerSummaryFromCapabilityMetadata(metadata, input)
	next := &NextActionHint{
		Kind:    "review",
		Title:   "Review Capability Signals",
		Message: summary,
		Metadata: map[string]any{
			"run_id": strings.TrimSpace(input.RunID),
		},
	}

	return &PostTurnActionResult{
		PlannerHint: &PlannerHint{
			Stage:    stage,
			Summary:  summary,
			Metadata: metadata,
		},
		NextActionHint: next,
	}, nil
}

func nextTurnPlanMetadata(toolMode string, toolNames []string, skillNames []string, resourceHintCount int, recallHit bool) map[string]any {
	plan := map[string]any{
		"tool_mode":           strings.TrimSpace(toolMode),
		"tool_allowlist":      append([]string(nil), toolNames...),
		"skill_names":         append([]string(nil), skillNames...),
		"resource_hint_count": resourceHintCount,
		"recall_hit":          recallHit,
	}
	return plan
}

func plannerSummaryFromCapabilityMetadata(metadata map[string]any, input PostTurnActionInput) string {
	mode, _ := metadata["resolved_tool_mode"].(string)
	skills, _ := metadata["resolved_skill_names"].([]string)
	resourceHintCount, _ := metadata["resource_hint_count"].(int)
	recallHit, _ := metadata["recall_hit"].(bool)

	parts := []string{}
	if strings.TrimSpace(mode) != "" {
		parts = append(parts, "tool mode="+strings.TrimSpace(mode))
	}
	if len(skills) > 0 {
		parts = append(parts, "skills="+strings.Join(skills, ","))
	}
	if resourceHintCount > 0 {
		parts = append(parts, "resource_hints="+itoa(resourceHintCount))
	}
	if recallHit {
		parts = append(parts, "recall_hit=true")
	}
	if len(parts) == 0 {
		if strings.TrimSpace(input.Summary) != "" {
			return strings.TrimSpace(input.Summary)
		}
		return "review capability signals for the completed turn"
	}
	return "review capability signals: " + strings.Join(parts, "; ")
}

func decodeStringAny(raw any) string {
	text, _ := raw.(string)
	return strings.TrimSpace(text)
}

func decodeStringSliceAny(raw any) []string {
	switch value := raw.(type) {
	case []string:
		out := make([]string, 0, len(value))
		for _, item := range value {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			out = append(out, item)
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				continue
			}
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			out = append(out, text)
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func decodeMapSliceAny(raw any) []map[string]any {
	switch value := raw.(type) {
	case []map[string]any:
		out := make([]map[string]any, 0, len(value))
		for _, item := range value {
			out = append(out, cloneAnyMap(item))
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]map[string]any, 0, len(value))
		for _, item := range value {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, cloneAnyMap(entry))
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func decodeBoolAny(raw any) bool {
	value, _ := raw.(bool)
	return value
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + (v % 10))
		v /= 10
	}
	return sign + string(buf[i:])
}
