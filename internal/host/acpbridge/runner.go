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
		Context:     map[string]any{},
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
	}, nil
}
