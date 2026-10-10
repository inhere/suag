package native

import (
	"context"
	"fmt"
	"github.com/inhere/suag/pkg/agentcore/tools"
	"strings"

	"github.com/inhere/suag/pkg/agentcore/api"
)

// Plan step statuses accepted by update_plan.
const (
	PlanPending    = "pending"
	PlanInProgress = "in_progress"
	PlanCompleted  = "completed"
)

// PlanStep is one item of the task list.
type PlanStep struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

// Plan is the full task list; every update replaces the previous one.
type Plan struct {
	Explanation string     `json:"explanation,omitempty"`
	Steps       []PlanStep `json:"plan"`
}

// PlanSink persists and publishes a validated plan. It is injected when the
// tool is built, so this package never depends on the session layer.
type PlanSink interface {
	RecordPlan(ctx context.Context, plan Plan) error
}

// UpdatePlanTool lets the model keep a structured task list. It never touches
// the workspace, so it is available in read-only modes too.
type UpdatePlanTool struct {
	sink PlanSink
}

// NewUpdatePlanTool creates the update_plan tool writing through sink.
func NewUpdatePlanTool(sink PlanSink) *UpdatePlanTool { return &UpdatePlanTool{sink: sink} }

func (t *UpdatePlanTool) Name() string { return "update_plan" }
func (t *UpdatePlanTool) Description() string {
	return "Create or replace the task list for the current work. Always send the complete list; " +
		"at most one step may be in_progress. Does not modify the workspace."
}

// ToolMeta marks the tool read-only: it only updates the session plan.
func (*UpdatePlanTool) ToolMeta() tools.Meta {
	return tools.Meta{Category: tools.CategoryThink, ReadOnly: true}
}

func (t *UpdatePlanTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"explanation": map[string]any{"type": "string", "description": "Optional note on why the plan changed"},
				"plan": map[string]any{
					"type":        "array",
					"description": "The complete task list (replaces the previous one)",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"step":   map[string]any{"type": "string", "description": "Short task description"},
							"status": map[string]any{"type": "string", "enum": []any{PlanPending, PlanInProgress, PlanCompleted}},
						},
						"required": []any{"step", "status"},
					},
				},
			},
			"required": []any{"plan"},
		},
	}
}

func (t *UpdatePlanTool) Execute(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	plan, err := parsePlan(call.Arguments)
	if err != nil {
		return nil, err
	}
	if t.sink == nil {
		return nil, fmt.Errorf("update_plan: no plan sink configured")
	}
	if err := t.sink.RecordPlan(ctx, plan); err != nil {
		return nil, fmt.Errorf("update_plan: %w", err)
	}
	return &api.ToolResult{CallID: call.CallID, Name: t.Name(), Content: fmt.Sprintf("Plan updated (%d steps)", len(plan.Steps))}, nil
}

// parsePlan validates the arguments: known status, non-empty step, and at most
// one in_progress step.
func parsePlan(args map[string]any) (Plan, error) {
	raw, ok := args["plan"].([]any)
	if !ok {
		return Plan{}, fmt.Errorf("update_plan: plan must be an array")
	}
	plan := Plan{Steps: make([]PlanStep, 0, len(raw))}
	plan.Explanation, _ = args["explanation"].(string)
	inProgress := 0
	for i, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			return Plan{}, fmt.Errorf("update_plan: plan[%d] must be an object", i)
		}
		step, _ := obj["step"].(string)
		status, _ := obj["status"].(string)
		if strings.TrimSpace(step) == "" {
			return Plan{}, fmt.Errorf("update_plan: plan[%d].step is required", i)
		}
		switch status {
		case PlanPending, PlanCompleted:
		case PlanInProgress:
			inProgress++
		default:
			return Plan{}, fmt.Errorf("update_plan: plan[%d].status %q must be pending, in_progress or completed", i, status)
		}
		plan.Steps = append(plan.Steps, PlanStep{Step: strings.TrimSpace(step), Status: status})
	}
	if inProgress > 1 {
		return Plan{}, fmt.Errorf("update_plan: at most one step may be in_progress, got %d", inProgress)
	}
	return plan, nil
}
