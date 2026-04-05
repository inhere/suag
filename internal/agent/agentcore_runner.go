package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/inhere/axon/internal/agentcoreadapters/internalllm"
	"github.com/inhere/axon/internal/agentcoreadapters/internaltools"
	"github.com/inhere/axon/internal/memory"
	"github.com/inhere/axon/pkg/agentcore"
)

// AgentcoreRunnerConfig configures how an internal agent is bridged onto agentcore.
type AgentcoreRunnerConfig struct {
	Runtime             agentcore.Runtime
	Storage             agentcore.Storage
	PromptBuilder       agentcore.PromptBuilder
	ContextBuilder      agentcore.ContextBuilder
	Hooks               []agentcore.Hook
	MaxSteps            int
	MaxToolCallsPerStep int
}

// AgentcoreRunner executes an internal agent by delegating the core loop to agentcore.
type AgentcoreRunner struct {
	agent   *Agent
	runtime agentcore.Runtime
	sm      *stateMachine
	cfg     AgentcoreRunnerConfig
}

// BuildAgentcoreConfig maps an internal Agent plus runner config to a public agentcore.Config.
func BuildAgentcoreConfig(a *Agent, cfg AgentcoreRunnerConfig) agentcore.Config {
	return agentcore.Config{
		Provider:            internalllm.NewProvider(a.LLM),
		Storage:             cfg.Storage,
		Tools:               internaltools.NewRegistry(a.Tools),
		PromptBuilder:       resolvePromptBuilder(a, cfg.PromptBuilder),
		ContextBuilder:      cfg.ContextBuilder,
		Hooks:               append([]agentcore.Hook(nil), cfg.Hooks...),
		MaxSteps:            cfg.MaxSteps,
		MaxToolCallsPerStep: cfg.MaxToolCallsPerStep,
	}
}

// NewAgentcoreRunner creates a Runner backed by the public agentcore runtime.
func NewAgentcoreRunner(a *Agent, cfg AgentcoreRunnerConfig) *AgentcoreRunner {
	rt := cfg.Runtime
	if rt == nil {
		rt = agentcore.New(BuildAgentcoreConfig(a, cfg))
	}

	initial := AgentPending
	if a != nil && a.State != "" {
		initial = a.State
	}

	return &AgentcoreRunner{
		agent:   a,
		runtime: rt,
		sm:      newStateMachine(initial),
		cfg:     cfg,
	}
}

// Run executes a task via agentcore and projects the result back into internal agent types.
func (r *AgentcoreRunner) Run(ctx context.Context, task *Task) (*TaskResult, error) {
	if r == nil || r.agent == nil {
		return nil, fmt.Errorf("agentcore runner: agent is nil")
	}
	if task == nil {
		return nil, fmt.Errorf("agentcore runner: task is nil")
	}
	if r.runtime == nil {
		return nil, fmt.Errorf("agentcore runner: runtime is nil")
	}

	if err := r.sm.Transition(AgentRunning); err != nil {
		return nil, fmt.Errorf("start agent: %w", err)
	}
	r.agent.State = AgentRunning

	runID := strings.TrimSpace(task.ID)
	if runID == "" {
		runID = fmt.Sprintf("task-%d", time.Now().UnixNano())
	}

	result, err := r.runtime.Run(ctx, agentcore.RunInput{
		RunID:       runID,
		AgentID:     r.agent.ID,
		Instruction: task.Description,
		Metadata:    buildRunMetadata(r.agent, task),
		MaxSteps:    r.cfg.MaxSteps,
	})
	if err != nil {
		_ = r.sm.Transition(AgentFailed)
		r.agent.State = AgentFailed
		return nil, err
	}

	if err := r.syncMemory(ctx, runID); err != nil {
		_ = r.sm.Transition(AgentFailed)
		r.agent.State = AgentFailed
		return nil, err
	}

	switch result.Status {
	case agentcore.RunCompleted:
		_ = r.sm.Transition(AgentCompleted)
		r.agent.State = AgentCompleted
		return &TaskResult{
			TaskID:  task.ID,
			Summary: extractSummary(result.Output),
			Output:  result.Output,
		}, nil
	case agentcore.RunYielded:
		_ = r.sm.Transition(AgentSuspended)
		r.agent.State = AgentSuspended
		yieldReason := ""
		if result.Yield != nil {
			yieldReason = result.Yield.Reason
		}
		return &TaskResult{
			TaskID:  task.ID,
			Summary: yieldReason,
			Output:  "",
		}, nil
	default:
		_ = r.sm.Transition(AgentFailed)
		r.agent.State = AgentFailed
		return nil, fmt.Errorf("agentcore run ended with status %s", result.Status)
	}
}

// GetState implements Runner.
func (r *AgentcoreRunner) GetState() AgentState {
	if r == nil || r.sm == nil {
		return AgentPending
	}
	return r.sm.Current()
}

// Suspend implements Runner.
func (r *AgentcoreRunner) Suspend(reason string) error {
	if r == nil || r.sm == nil || r.agent == nil {
		return fmt.Errorf("agentcore runner is not initialized")
	}
	if err := r.sm.Transition(AgentSuspended); err != nil {
		return err
	}
	r.agent.State = AgentSuspended
	if strings.TrimSpace(reason) != "" && r.agent.Memory != nil {
		r.agent.Memory.AddMessage("system", "[Suspended]: "+reason)
	}
	return nil
}

// Resume implements Runner.
func (r *AgentcoreRunner) Resume(instruction string) error {
	if r == nil || r.sm == nil || r.agent == nil {
		return fmt.Errorf("agentcore runner is not initialized")
	}
	if err := r.sm.Transition(AgentRunning); err != nil {
		return err
	}
	r.agent.State = AgentRunning
	if strings.TrimSpace(instruction) != "" && r.agent.Memory != nil {
		r.agent.Memory.AddMessage("system", "[Human instruction]: "+instruction)
	}
	return nil
}

func (r *AgentcoreRunner) syncMemory(ctx context.Context, runID string) error {
	if r == nil || r.agent == nil || r.agent.Memory == nil {
		return nil
	}

	cfg := BuildAgentcoreConfig(r.agent, r.cfg)
	if cfg.Storage == nil {
		return nil
	}

	messages, err := cfg.Storage.LoadMessages(ctx, runID)
	if err != nil {
		return err
	}

	transcript := compactMessages(messages)
	existing := r.agent.Memory.GetMessages()
	if !isMemoryPrefix(existing, transcript) {
		return nil
	}

	for _, msg := range transcript[len(existing):] {
		r.agent.Memory.AddMessage(msg.Role, msg.Content)
	}
	return nil
}

func resolvePromptBuilder(a *Agent, builder agentcore.PromptBuilder) agentcore.PromptBuilder {
	if builder != nil {
		return builder
	}
	if a == nil || a.Soul == nil || strings.TrimSpace(a.Soul.Persona) == "" {
		return nil
	}
	return soulPromptBuilder{persona: a.Soul.Persona}
}

func buildRunMetadata(a *Agent, task *Task) map[string]any {
	metadata := map[string]any{}
	if task != nil {
		if strings.TrimSpace(task.Title) != "" {
			metadata["task_title"] = task.Title
		}
		if strings.TrimSpace(task.TraceID) != "" {
			metadata["trace_id"] = task.TraceID
		}
		if strings.TrimSpace(task.ParentID) != "" {
			metadata["parent_task_id"] = task.ParentID
		}
	}
	if a != nil {
		if strings.TrimSpace(a.Name) != "" {
			metadata["agent_name"] = a.Name
		}
		if strings.TrimSpace(a.ParentID) != "" {
			metadata["parent_agent_id"] = a.ParentID
		}
		if strings.TrimSpace(a.TraceID) != "" {
			metadata["agent_trace_id"] = a.TraceID
		}
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func compactMessages(messages []agentcore.Message) []memory.Message {
	if len(messages) == 0 {
		return nil
	}

	out := make([]memory.Message, 0, len(messages))
	for _, msg := range messages {
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		out = append(out, memory.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	return out
}

func isMemoryPrefix(existing []memory.Message, transcript []memory.Message) bool {
	if len(existing) > len(transcript) {
		return false
	}
	for i := range existing {
		if existing[i].Role != transcript[i].Role || existing[i].Content != transcript[i].Content {
			return false
		}
	}
	return true
}

type soulPromptBuilder struct {
	persona string
}

func (b soulPromptBuilder) BuildSystemPrompt(context.Context, *agentcore.RunState) (string, error) {
	return b.persona, nil
}
