package acpbridge

import "time"

type DispatchState string

const (
	StateRunning   DispatchState = "running"
	StateCompleted DispatchState = "completed"
	StateFailed    DispatchState = "failed"
)

type DispatchRequest struct {
	SessionID   string
	TurnID      string
	RunID       string
	AgentID     string
	Adapter     string
	Instruction string
	Workspace   string
	Context     map[string]any
	Emit        func(event DispatchEvent) error
}

type DispatchResult struct {
	State           DispatchState
	Summary         string
	Error           string
	Artifacts       []string
	Metadata        map[string]any
	RemoteAgentName string
	RemoteSessionID string
	RemoteTaskID    string
	LastHeartbeatAt time.Time
}

type DispatchEvent struct {
	Type    string
	Payload map[string]any
}
