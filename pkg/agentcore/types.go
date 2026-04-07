package agentcore

// RunInput captures the public input contract for a single agent run.
type RunInput struct {
	RunID       string
	AgentID     string
	Instruction string
	Metadata    map[string]any
	Stream      StreamFunc
	MaxSteps    int
	ResumeMode  ResumeMode
}

// RunStatus describes the lifecycle state of a run.
type RunStatus string

const (
	RunIdle      RunStatus = "idle"
	RunRunning   RunStatus = "running"
	RunYielded   RunStatus = "yielded"
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
)

// ResumeMode describes how a runtime should source state when continuing a run.
type ResumeMode string

const (
	ResumeAuto      ResumeMode = "auto"
	ResumeFromState ResumeMode = "from_state"
	ResumeWithInput ResumeMode = "with_input"
)

// Message is the public conversation unit exchanged with prompt, context, and model layers.
type Message struct {
	Role       string
	Content    string
	ToolCall   *ToolCall
	ToolResult *ToolResult
	Metadata   map[string]any
}

// Artifact is a minimal portable output produced by tools or models.
type Artifact struct {
	Kind     string
	URI      string
	Content  string
	Metadata map[string]any
}

// Usage tracks model token consumption.
type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// Yield captures a paused runtime state that requires external input.
type Yield struct {
	Reason   string
	ResumeOK bool
	Metadata map[string]any
}

// RunState is the primary public runtime state contract shared across storage and builders.
type RunState struct {
	RunID     string
	AgentID   string
	Status    RunStatus
	Step      int
	Messages  []Message
	Artifacts []Artifact
	Metadata  map[string]any
	Scratch   map[string]any
	Usage     Usage
	LastError string
}

// RunResult is the public outcome contract returned by a runtime execution.
type RunResult struct {
	RunID  string
	Status RunStatus
	Output string
	Steps  int
	Usage  Usage
	Yield  *Yield
}
