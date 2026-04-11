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

	result, err := r.bridge.Dispatch(ctx, DispatchRequest{
		SessionID:   in.SessionID,
		TurnID:      in.TurnID,
		RunID:       in.RunID,
		AgentID:     in.AgentID,
		Adapter:     r.adapterName,
		Instruction: in.Instruction,
		Workspace:   r.workspace,
		Context:     r.buildDispatchContext(in.Context),
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

	return session.RuntimeResult{
		Summary: result.Summary,
		ExternalTask: &session.ExternalTask{
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
