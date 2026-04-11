package acpbridge

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
}

type DispatchResult struct {
	State     DispatchState
	Summary   string
	Error     string
	Artifacts []string
}
