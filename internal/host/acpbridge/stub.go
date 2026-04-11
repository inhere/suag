package acpbridge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/inhere/axon/internal/host/session"
)

type StubBridgeConfig struct {
	RemoteAgentName string
	Now             func() time.Time
}

type StubBridge struct {
	remoteAgentName string
	now             func() time.Time
}

func NewStubBridge(cfg StubBridgeConfig) *StubBridge {
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	remoteAgentName := strings.TrimSpace(cfg.RemoteAgentName)
	if remoteAgentName == "" {
		remoteAgentName = "stub-agent"
	}

	return &StubBridge{
		remoteAgentName: remoteAgentName,
		now:             nowFn,
	}
}

func (b *StubBridge) Dispatch(_ context.Context, req DispatchRequest) (DispatchResult, error) {
	if b == nil {
		return DispatchResult{}, fmt.Errorf("stub bridge is required")
	}
	if strings.TrimSpace(req.Instruction) == "" {
		return DispatchResult{}, fmt.Errorf("instruction is required")
	}

	now := b.now().UTC()
	remoteSessionID := session.NewSessionID()
	remoteTaskID := session.NewExternalTaskID()

	return DispatchResult{
		State:           StateCompleted,
		Summary:         fmt.Sprintf("stub completed via %s: %s", strings.TrimSpace(req.Adapter), strings.TrimSpace(req.Instruction)),
		Artifacts:       []string{"artifact://stub/summary"},
		RemoteAgentName: b.remoteAgentName,
		RemoteSessionID: remoteSessionID,
		RemoteTaskID:    remoteTaskID,
		LastHeartbeatAt: now,
	}, nil
}
