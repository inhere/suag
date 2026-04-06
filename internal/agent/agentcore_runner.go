package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/inhere/axon/internal/agentcoreadapters/internalllm"
	"github.com/inhere/axon/internal/agentcoreadapters/internalmemory"
	"github.com/inhere/axon/internal/agentcoreadapters/internaltools"
	"github.com/inhere/axon/internal/hitl"
	"github.com/inhere/axon/internal/tracing"
	"github.com/inhere/axon/pkg/agentcore"
)

// AgentcoreRunnerConfig configures how an internal agent is bridged onto agentcore.
type AgentcoreRunnerConfig struct {
	Runtime             agentcore.Runtime
	Storage             agentcore.Storage
	PromptBuilder       agentcore.PromptBuilder
	ContextBuilder      agentcore.ContextBuilder
	Hooks               []agentcore.Hook
	Tracer              tracing.Tracer
	HITL                hitl.Manager
	FailThreshold       int
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
		Memory:              resolveMemory(a),
		Tools:               internaltools.NewRegistry(a.Tools),
		PromptBuilder:       resolvePromptBuilder(a, cfg.PromptBuilder),
		ContextBuilder:      cfg.ContextBuilder,
		Hooks:               buildAgentcoreHooks(a, cfg),
		MaxSteps:            normalizeMaxSteps(cfg.MaxSteps),
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
		MaxSteps:    normalizeMaxSteps(r.cfg.MaxSteps),
	})
	if err != nil {
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
		if strings.TrimSpace(task.ID) != "" {
			metadata["task_id"] = task.ID
		}
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

func resolveMemory(a *Agent) agentcore.Memory {
	if a == nil || a.Memory == nil {
		return nil
	}
	return internalmemory.New(a.Memory)
}

type soulPromptBuilder struct {
	persona string
}

func (b soulPromptBuilder) BuildSystemPrompt(context.Context, *agentcore.RunState) (string, error) {
	return b.persona, nil
}

func buildAgentcoreHooks(a *Agent, cfg AgentcoreRunnerConfig) []agentcore.Hook {
	hooks := make([]agentcore.Hook, 0, len(cfg.Hooks)+2)
	if cfg.Tracer != nil && a != nil {
		hooks = append(hooks, newTracingHook(a.ID, cfg.Tracer))
	}
	if cfg.HITL != nil {
		hooks = append(hooks, newHITLHook(a, cfg))
	}
	hooks = append(hooks, cfg.Hooks...)
	return hooks
}

type tracingHook struct {
	agentcore.NopHook
	agentID  string
	tracer   tracing.Tracer
	taskCtx  context.Context
	taskSpan *tracing.Span
	llmSpan  *tracing.Span
	toolSpan *tracing.Span
}

func newTracingHook(agentID string, tracer tracing.Tracer) agentcore.Hook {
	return &tracingHook{
		agentID: agentID,
		tracer:  tracer,
	}
}

func (h *tracingHook) BeforeRun(ctx context.Context, event *agentcore.RunEvent, _ *agentcore.Control) error {
	if h == nil || h.tracer == nil {
		return nil
	}
	h.taskCtx, h.taskSpan = h.tracer.StartSpan(ctx, h.agentID, "task")
	if h.taskSpan != nil && event != nil && event.Input != nil {
		h.taskSpan.Input = event.Input.Instruction
	}
	return nil
}

func (h *tracingHook) BeforeModel(ctx context.Context, event *agentcore.ModelEvent, _ *agentcore.Control) error {
	if h == nil || h.tracer == nil {
		return nil
	}
	baseCtx := ctx
	if h.taskCtx != nil {
		baseCtx = h.taskCtx
	}
	_, h.llmSpan = h.tracer.StartSpan(baseCtx, h.agentID, "llm_call")
	if h.llmSpan != nil && event != nil && event.Request != nil {
		h.llmSpan.Input = summarizeMessages(event.Request.Messages)
	}
	return nil
}

func (h *tracingHook) AfterModel(_ context.Context, event *agentcore.ModelEvent, _ *agentcore.Control) error {
	if h == nil || h.tracer == nil || h.llmSpan == nil {
		return nil
	}
	if event != nil && event.Result != nil {
		h.llmSpan.Output = event.Result.Content
	}
	h.tracer.FinishSpan(h.llmSpan, nil)
	h.llmSpan = nil
	return nil
}

func (h *tracingHook) BeforeTool(ctx context.Context, event *agentcore.ToolEvent, _ *agentcore.Control) error {
	if h == nil || h.tracer == nil {
		return nil
	}
	baseCtx := ctx
	if h.taskCtx != nil {
		baseCtx = h.taskCtx
	}
	_, h.toolSpan = h.tracer.StartSpan(baseCtx, h.agentID, "tool_call")
	if h.toolSpan != nil && event != nil && event.Call != nil {
		h.toolSpan.Meta["tool"] = event.Call.Name
	}
	return nil
}

func (h *tracingHook) AfterTool(_ context.Context, event *agentcore.ToolEvent, _ *agentcore.Control) error {
	if h == nil || h.tracer == nil || h.toolSpan == nil {
		return nil
	}
	var toolErr error
	if event != nil && event.Result != nil {
		h.toolSpan.Output = event.Result.Content
		if isToolErrorResult(event.Result) {
			toolErr = fmt.Errorf("%s", event.Result.Content)
		}
	}
	h.tracer.FinishSpan(h.toolSpan, toolErr)
	h.toolSpan = nil
	return nil
}

func (h *tracingHook) AfterRun(_ context.Context, event *agentcore.RunEvent, _ *agentcore.Control) error {
	if h == nil || h.tracer == nil || h.taskSpan == nil {
		return nil
	}
	if event != nil && event.Result != nil {
		h.taskSpan.Output = event.Result.Output
	}
	h.tracer.FinishSpan(h.taskSpan, nil)
	h.taskSpan = nil
	return nil
}

func (h *tracingHook) OnError(_ context.Context, event *agentcore.ErrorEvent, _ *agentcore.Control) error {
	if h == nil || h.tracer == nil {
		return nil
	}
	if h.llmSpan != nil {
		h.tracer.FinishSpan(h.llmSpan, event.Err)
		h.llmSpan = nil
	}
	if h.toolSpan != nil {
		h.tracer.FinishSpan(h.toolSpan, event.Err)
		h.toolSpan = nil
	}
	if h.taskSpan != nil {
		h.tracer.FinishSpan(h.taskSpan, event.Err)
		h.taskSpan = nil
	}
	return nil
}

type hitlHook struct {
	agentcore.NopHook
	agent        *Agent
	storage      agentcore.Storage
	memory       agentcore.Memory
	manager      hitl.Manager
	failThresh   int
	failureCount int
	pendingCall  *agentcore.ToolCall
}

func newHITLHook(a *Agent, cfg AgentcoreRunnerConfig) agentcore.Hook {
	threshold := cfg.FailThreshold
	if threshold <= 0 {
		threshold = defaultFailThresh
	}
	return &hitlHook{
		agent:      a,
		storage:    cfg.Storage,
		memory:     resolveMemory(a),
		manager:    cfg.HITL,
		failThresh: threshold,
	}
}

func (h *hitlHook) AfterTool(_ context.Context, event *agentcore.ToolEvent, _ *agentcore.Control) error {
	if h == nil || event == nil || event.Call == nil || event.Result == nil {
		return nil
	}
	if isToolErrorResult(event.Result) {
		h.failureCount++
		copied := *event.Call
		h.pendingCall = &copied
		return nil
	}

	h.failureCount = 0
	h.pendingCall = nil
	return nil
}

func (h *hitlHook) BeforeContextBuild(ctx context.Context, event *agentcore.ContextEvent, _ *agentcore.Control) error {
	if h == nil || h.manager == nil || h.pendingCall == nil || h.failureCount < h.failThresh || event == nil || event.State == nil {
		return nil
	}

	taskID, _ := event.State.Metadata["task_id"].(string)
	reason := fmt.Sprintf("tool %q failed %d times in a row", h.pendingCall.Name, h.failThresh)
	if h.agent != nil {
		h.agent.State = AgentSuspended
	}
	resp, err := h.manager.RequestIntervention(ctx, event.AgentID, taskID, reason, pendingToolErrorMessage(event))
	if err != nil {
		return err
	}

	h.failureCount = 0
	h.pendingCall = nil
	if h.agent != nil {
		h.agent.State = AgentRunning
	}

	switch resp.Action {
	case hitl.ActionAbandon:
		return fmt.Errorf("task abandoned by operator: %s", resp.Instruction)
	default:
		instruction := strings.TrimSpace(resp.Instruction)
		if instruction == "" {
			return nil
		}
		msg := agentcore.Message{
			Role:    "system",
			Content: "[Human instruction]: " + instruction,
		}
		event.State.Messages = append(event.State.Messages, msg)
		if h.storage != nil {
			if err := h.storage.AppendMessages(ctx, event.RunID, msg); err != nil {
				return err
			}
		}
		if h.memory != nil {
			if err := h.memory.ObserveMessages(ctx, event.State, msg); err != nil {
				return err
			}
		}
		return nil
	}
}

func summarizeMessages(messages []agentcore.Message) string {
	if len(messages) == 0 {
		return ""
	}
	last := messages[len(messages)-1]
	return last.Content
}

func isToolErrorResult(result *agentcore.ToolResult) bool {
	if result == nil {
		return false
	}
	if flag, _ := result.Metadata["tool_error"].(bool); flag {
		return true
	}
	return strings.HasPrefix(result.Content, "tool error:")
}

func pendingToolErrorMessage(event *agentcore.ContextEvent) string {
	if event == nil || event.State == nil || len(event.State.Messages) == 0 {
		return ""
	}
	for i := len(event.State.Messages) - 1; i >= 0; i-- {
		msg := event.State.Messages[i]
		if msg.Role == "tool" && strings.TrimSpace(msg.Content) != "" {
			return msg.Content
		}
	}
	return ""
}
