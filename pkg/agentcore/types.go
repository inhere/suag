package agentcore

import "time"

// RunInput captures the public input contract for a single agent run.
type RunInput struct {
	RunID       string
	AgentID     string
	Instruction string
	Metadata    map[string]any
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

// ResumeMode describes how a runtime should treat previously persisted state.
type ResumeMode string

const (
	ResumeDisabled ResumeMode = "disabled"
	ResumeAuto     ResumeMode = "auto"
	ResumeRequired ResumeMode = "required"
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
	Metadata map[string]any
}

// RunRecord is the portable persisted form of a run state.
type RunRecord struct {
	Input       RunInput
	Status      RunStatus
	Messages    []Message
	Result      *GenerateResult
	Yield       *Yield
	Metadata    map[string]any
	StartedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt *time.Time
}
