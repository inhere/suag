package api

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

// ToolBatchMetaKey is the Message.Metadata key (string) shared by the
// assistant tool-call messages of one model reply with several calls. The
// runtime stores each call with its result right after it; providers use the
// key to send such a reply back as one assistant message with all its calls
// followed by the results, so the history shows parallel calls as they were.
const ToolBatchMetaKey = "suag.tool_batch"

// Message is the public conversation unit exchanged with prompt, context, and model layers.
type Message struct {
	Role       string
	Content    string
	ToolCall   *ToolCall
	ToolResult *ToolResult
	Metadata   map[string]any
	// Reasoning is the model's reasoning text for an assistant message (e.g.
	// OpenAI-compatible reasoning_content). It is persisted with the message so
	// providers whose APIs require it can send it back on later requests.
	Reasoning string `json:",omitempty"`
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
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	CacheReadTokens  int     `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int     `json:"cache_write_tokens,omitempty"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
}

// Add returns the field-wise sum of u and o. Providers report usage per model
// call; a run or session total is the sum of its calls.
func (u Usage) Add(o Usage) Usage {
	u.InputTokens += o.InputTokens
	u.OutputTokens += o.OutputTokens
	u.TotalTokens += o.TotalTokens
	u.CacheReadTokens += o.CacheReadTokens
	u.CacheWriteTokens += o.CacheWriteTokens
	u.CostUSD += o.CostUSD
	return u
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
	// WrapUp is why the run ended with a wrap-up answer ("max_steps", or the
	// reason a hook gave Control.RequestWrapUp); empty for a normal answer.
	WrapUp string
}
