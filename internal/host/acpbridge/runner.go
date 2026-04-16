package acpbridge

import (
	"context"
	"fmt"
	"strings"

	"github.com/inhere/axon/internal/host/session"
)

type RunnerAdapterConfig struct {
	AdapterName string
	Workspace   string
}

type RunnerAdapter struct {
	bridge      Bridge
	adapterName string
	workspace   string
}

func NewRunnerAdapter(bridge Bridge, cfg RunnerAdapterConfig) *RunnerAdapter {
	return &RunnerAdapter{
		bridge:      bridge,
		adapterName: strings.TrimSpace(cfg.AdapterName),
		workspace:   strings.TrimSpace(cfg.Workspace),
	}
}

func (r *RunnerAdapter) Run(ctx context.Context, in session.RuntimeRequest) (session.RuntimeResult, error) {
	if r == nil || r.bridge == nil {
		return session.RuntimeResult{}, fmt.Errorf("acp bridge is required")
	}
	if in.Emit != nil {
		if err := in.Emit(session.RuntimeEvent{
			EventType: session.EventRunStatus,
			Payload: map[string]any{
				"state": string(session.RunDispatching),
			},
		}); err != nil {
			return session.RuntimeResult{}, err
		}
	}

	emittedExternal := false
	result, err := r.bridge.Dispatch(ctx, DispatchRequest{
		SessionID:   in.SessionID,
		TurnID:      in.TurnID,
		RunID:       in.RunID,
		AgentID:     in.AgentID,
		Adapter:     r.adapterName,
		Instruction: in.Instruction,
		Workspace:   r.workspace,
		Context:     r.buildDispatchContext(in.Context),
		Emit:        r.buildDispatchEmitter(in.Emit, contextStringValue(in.Context, "external_task_id"), &emittedExternal),
	})
	if err != nil {
		return session.RuntimeResult{}, err
	}
	if result.State == StateFailed {
		if strings.TrimSpace(result.Error) != "" {
			return session.RuntimeResult{}, fmt.Errorf("%s", result.Error)
		}
		return session.RuntimeResult{}, fmt.Errorf("acp dispatch failed")
	}

	waitState := session.RunState("")
	if result.State == StateRunning {
		waitState = session.RunExternal
		if !emittedExternal && in.Emit != nil {
			if err := in.Emit(session.RuntimeEvent{
				EventType: session.EventRunStatus,
				Payload: map[string]any{
					"state": string(session.RunExternal),
				},
			}); err != nil {
				return session.RuntimeResult{}, err
			}
		}
	}

	return session.RuntimeResult{
		Summary:   result.Summary,
		WaitState: waitState,
		ExternalTask: &session.ExternalTask{
			ExternalTaskID:  contextStringValue(in.Context, "external_task_id"),
			Adapter:         r.adapterName,
			RemoteAgentName: result.RemoteAgentName,
			RemoteSessionID: result.RemoteSessionID,
			RemoteTaskID:    result.RemoteTaskID,
			State:           string(result.State),
			LastHeartbeatAt: result.LastHeartbeatAt,
			ResultSummary:   result.Summary,
			Metadata: mergeMetadata(map[string]any{
				"workspace": r.workspace,
				"artifacts": result.Artifacts,
			}, result.Metadata),
		},
	}, nil
}

func mergeMetadata(dst map[string]any, src map[string]any) map[string]any {
	if dst == nil && src == nil {
		return map[string]any{}
	}
	if dst == nil {
		dst = map[string]any{}
	}
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func (r *RunnerAdapter) buildDispatchEmitter(emit func(event session.RuntimeEvent) error, externalTaskID string, emittedExternal *bool) func(event DispatchEvent) error {
	if emit == nil {
		return nil
	}

	return func(event DispatchEvent) error {
		runtimeEvents := mapDispatchEvents(event, externalTaskID)
		if len(runtimeEvents) > 0 && emittedExternal != nil {
			*emittedExternal = true
		}
		for _, runtimeEvent := range runtimeEvents {
			if err := emit(runtimeEvent); err != nil {
				return err
			}
		}
		return nil
	}
}

func (r *RunnerAdapter) buildDispatchContext(src map[string]any) map[string]any {
	out := mergeMetadata(map[string]any{}, src)

	rawTasks, ok := src["resume_external_tasks"]
	if !ok {
		return out
	}
	tasks, ok := rawTasks.([]session.ExternalTask)
	if !ok {
		return out
	}

	for _, task := range tasks {
		if strings.TrimSpace(task.Adapter) != r.adapterName {
			continue
		}
		if strings.TrimSpace(task.RemoteSessionID) == "" {
			continue
		}
		out["resume_remote_session_id"] = task.RemoteSessionID
		if strings.TrimSpace(task.RemoteTaskID) != "" {
			out["resume_remote_task_id"] = task.RemoteTaskID
		}
		out["resume_external_task_id"] = task.ExternalTaskID
	}
	return out
}

func mapDispatchEvents(event DispatchEvent, externalTaskID string) []session.RuntimeEvent {
	payload := mergeMetadata(map[string]any{}, event.Payload)
	if strings.TrimSpace(externalTaskID) != "" {
		payload["externalTaskId"] = externalTaskID
	}

	switch strings.TrimSpace(event.Type) {
	case "external.activity":
		return []session.RuntimeEvent{
			{
				EventType: session.EventRunStatus,
				Payload: map[string]any{
					"state": string(session.RunExternal),
				},
			},
			{
				EventType: session.EventRunExternalActivity,
				Payload:   payload,
			},
		}
	case "external.message.delta":
		return []session.RuntimeEvent{{
			EventType: session.EventRunExternalMessageDelta,
			Payload:   payload,
		}}
	case "external.message.end":
		return []session.RuntimeEvent{{
			EventType: session.EventRunExternalMessageEnd,
			Payload:   payload,
		}}
	default:
		return nil
	}
}

func contextStringValue(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	raw, ok := values[key]
	if !ok {
		return ""
	}
	text, _ := raw.(string)
	return strings.TrimSpace(text)
}
