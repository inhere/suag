package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/inhere/axon/internal/ops/hitl"
	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/internal/platform/memory"
	"github.com/inhere/axon/internal/platform/tracing"
	"github.com/inhere/axon/pkg/agentcore"
)

const (
	defaultMaxSteps   = 30
	defaultFailThresh = 3 // HITL trigger after N consecutive tool failures
)

// ReactRunner drives the legacy ReAct (Reason + Act) loop for a single Agent.
//
// Deprecated: prefer AgentcoreRunner for new integrations. ReactRunner remains
// as an explicit fallback path and parity baseline while internal callers
// finish converging on agentcore.
type ReactRunner struct {
	agent               *Agent
	sm                  *stateMachine
	maxSteps            int
	failThresh          int
	tracer              tracing.Tracer
	hitlMgr             hitl.Manager
	resumePolicy        agentcore.ResumePolicy
	toolExecutionPolicy agentcore.ToolExecutionPolicy
	errorPolicy         agentcore.ErrorPolicy
}

// ReactRunnerConfig configures the legacy ReactRunner bridge behavior.
type ReactRunnerConfig struct {
	MaxSteps            int
	FailThreshold       int
	Tracer              tracing.Tracer
	HITL                hitl.Manager
	ResumePolicy        agentcore.ResumePolicy
	ToolExecutionPolicy agentcore.ToolExecutionPolicy
	ErrorPolicy         agentcore.ErrorPolicy
}

// NewReactRunner creates a legacy ReactRunner for the given agent.
//
// Deprecated: prefer NewAgentcoreRunner for new integrations. Keep this
// constructor only for explicit fallback wiring and parity tests.
func NewReactRunner(a *Agent, maxSteps int, tracer tracing.Tracer, hitlMgr hitl.Manager) *ReactRunner {
	return NewReactRunnerWithConfig(a, ReactRunnerConfig{
		MaxSteps: maxSteps,
		Tracer:   tracer,
		HITL:     hitlMgr,
	})
}

// NewReactRunnerWithConfig creates a legacy ReactRunner with optional agentcore-compatible contracts.
func NewReactRunnerWithConfig(a *Agent, cfg ReactRunnerConfig) *ReactRunner {
	maxSteps := normalizeMaxSteps(cfg.MaxSteps)
	failThreshold := cfg.FailThreshold
	if failThreshold <= 0 {
		failThreshold = defaultFailThresh
	}
	return &ReactRunner{
		agent:               a,
		sm:                  newStateMachine(a.State),
		maxSteps:            maxSteps,
		failThresh:          failThreshold,
		tracer:              cfg.Tracer,
		hitlMgr:             cfg.HITL,
		resumePolicy:        resolveReactResumePolicy(cfg.ResumePolicy),
		toolExecutionPolicy: resolveReactToolExecutionPolicy(cfg.ToolExecutionPolicy),
		errorPolicy:         resolveReactErrorPolicy(cfg.ErrorPolicy),
	}
}

// Run executes the ReAct loop until the task is complete, failed, or suspended.
func (r *ReactRunner) Run(ctx context.Context, task *Task) (*TaskResult, error) {
	if err := r.sm.Transition(AgentRunning); err != nil {
		return nil, fmt.Errorf("start agent: %w", err)
	}
	r.agent.State = AgentRunning

	// Build the task span under the trace carried in ctx.
	ctx, taskSpan := r.tracer.StartSpan(ctx, r.agent.ID, "task")
	taskSpan.Input = task.Description
	defer func() { r.tracer.FinishSpan(taskSpan, nil) }()

	// Seed memory with the task description.
	r.agent.Memory.AddMessage("user", task.Description)

	// Inject soul persona as system context.
	system := ""
	if r.agent.Soul != nil {
		system = r.agent.Soul.Persona
	}

	consecutiveFails := 0
	lastFailedTool := ""

	for step := 0; step < r.maxSteps; step++ {
		slog.Debug("react step", "agent", r.agent.ID, "step", step)

		// ── Retrieve context from memory ──────────────────────────────────────
		messages, err := r.agent.Memory.GetContext(ctx, 8000)
		if err != nil {
			return nil, fmt.Errorf("get context: %w", err)
		}

		// ── LLM call ──────────────────────────────────────────────────────────
		ctx, llmSpan := r.tracer.StartSpan(ctx, r.agent.ID, "llm_call")
		llmResp, llmErr := r.agent.LLM.Complete(ctx, &llm.CompletionRequest{
			Messages:    memToLLMMessages(messages),
			Tools:       r.agent.Tools.Schemas(),
			MaxTokens:   4096,
			Temperature: 0.2,
			System:      system,
		})
		if llmErr != nil {
			r.tracer.FinishSpan(llmSpan, llmErr)
			result, handledErr := r.handleRecoverableError(ctx, task, step, "model", &agentcore.ProviderError{Err: llmErr})
			if result != nil || handledErr != nil {
				return result, handledErr
			}
			return nil, fmt.Errorf("llm call step %d: %w", step, llmErr)
		}
		llmSpan.Output = llmResp.Content
		r.tracer.FinishSpan(llmSpan, nil)

		// ── Tool call branch ──────────────────────────────────────────────────
		if llmResp.IsToolCall() {
			if err := r.toolExecutionPolicy.ValidateToolCalls(ctx, r.currentRunState(step), toAgentcoreToolCalls(llmResp.ToolCalls)); err != nil {
				_ = r.sm.Transition(AgentFailed)
				r.agent.State = AgentFailed
				return nil, err
			}
			for _, tc := range llmResp.ToolCalls {
				result, toolErr := r.executeToolCall(ctx, tc)
				if toolErr != nil {
					decision := r.toolExecutionPolicy.HandleToolError(ctx, r.currentRunState(step), toAgentcoreToolCall(tc), toolErr)
					if decision.Fail != nil {
						_ = r.sm.Transition(AgentFailed)
						r.agent.State = AgentFailed
						return nil, decision.Fail
					}

					errMsg := fmt.Sprintf("tool %q error: %s", tc.Name, toolErr.Error())
					if decision.Result != nil && strings.TrimSpace(decision.Result.Content) != "" {
						errMsg = decision.Result.Content
					}
					if lastFailedTool != tc.Name {
						consecutiveFails = 0
					}
					lastFailedTool = tc.Name
					consecutiveFails++
					slog.Warn("tool call failed",
						"agent", r.agent.ID,
						"tool", tc.Name,
						"err", toolErr,
						"consecutive", consecutiveFails,
					)

					r.agent.Memory.AddMessage("tool", errMsg)

					if consecutiveFails >= r.failThresh && r.hitlMgr != nil {
						if hitlErr := r.triggerHITL(ctx, task, tc.Name, errMsg); hitlErr != nil {
							_ = r.sm.Transition(AgentFailed)
							r.agent.State = AgentFailed
							return nil, fmt.Errorf("hitl: %w", hitlErr)
						}
						consecutiveFails = 0
					}
					continue
				}
				consecutiveFails = 0
				lastFailedTool = ""
				r.agent.Memory.AddMessage("tool", result)
			}
			continue
		}

		// ── Direct answer / task complete ─────────────────────────────────────
		if llmResp.Content != "" {
			r.agent.Memory.AddMessage("assistant", llmResp.Content)
		}

		if llmResp.IsComplete() {
			_ = r.sm.Transition(AgentCompleted)
			r.agent.State = AgentCompleted
			taskSpan.Output = llmResp.Content
			return &TaskResult{
				TaskID:  task.ID,
				Summary: extractSummary(llmResp.Content),
				Output:  llmResp.Content,
			}, nil
		}
	}

	// Exceeded max steps.
	_ = r.sm.Transition(AgentFailed)
	r.agent.State = AgentFailed
	return nil, fmt.Errorf("max steps (%d) reached for task %q", r.maxSteps, task.ID)
}

// GetState implements Runner.
func (r *ReactRunner) GetState() AgentState { return r.sm.Current() }

// Suspend implements Runner.
func (r *ReactRunner) Suspend(reason string) error {
	if err := r.sm.Transition(AgentSuspended); err != nil {
		return err
	}
	r.agent.State = AgentSuspended
	slog.Info("agent suspended", "agent", r.agent.ID, "reason", reason)
	return nil
}

// Resume implements Runner.
func (r *ReactRunner) Resume(instruction string) error {
	input := agentcore.RunInput{
		Instruction: instruction,
		ResumeMode:  agentcore.ResumeFromState,
	}
	if strings.TrimSpace(instruction) != "" {
		input.ResumeMode = agentcore.ResumeWithInput
	}
	if err := r.resumePolicy.AllowResume(context.Background(), input, r.currentRunState(0)); err != nil {
		return err
	}
	if err := r.sm.Transition(AgentRunning); err != nil {
		return err
	}
	r.agent.State = AgentRunning
	if r.resumePolicy.ShouldAppendInstruction(input, r.currentRunState(0)) {
		r.agent.Memory.AddMessage("system", "[Human instruction]: "+instruction)
	}
	return nil
}

// ─── internals ────────────────────────────────────────────────────────────────

func (r *ReactRunner) executeToolCall(ctx context.Context, tc llm.ToolCall) (string, error) {
	ctx, span := r.tracer.StartSpan(ctx, r.agent.ID, "tool_call")
	span.Meta["tool"] = tc.Name
	result, err := r.agent.Tools.Execute(ctx, tc.Name, tc.Arguments)
	r.tracer.FinishSpan(span, err)
	if err != nil {
		return "", err
	}
	span.Output = result
	return result, nil
}

func (r *ReactRunner) triggerHITL(ctx context.Context, task *Task, toolName, errMsg string) error {
	slog.Warn("triggering HITL", "agent", r.agent.ID, "tool", toolName)
	_ = r.sm.Transition(AgentSuspended)
	r.agent.State = AgentSuspended

	resp, err := r.hitlMgr.RequestIntervention(ctx,
		r.agent.ID,
		task.ID,
		fmt.Sprintf("tool %q failed %d times in a row", toolName, r.failThresh),
		errMsg,
	)
	if err != nil {
		return err
	}

	switch resp.Action {
	case hitl.ActionAbandon:
		return fmt.Errorf("task abandoned by operator: %s", resp.Instruction)
	default:
		return r.Resume(resp.Instruction)
	}
}

func (r *ReactRunner) handleRecoverableError(ctx context.Context, task *Task, step int, stage string, err error) (*TaskResult, error) {
	if r == nil {
		return nil, err
	}
	state := r.currentRunState(step)
	decision := r.errorPolicy.HandleRecoverable(ctx, state, &agentcore.ErrorEvent{
		RunID:   state.RunID,
		AgentID: state.AgentID,
		Stage:   stage,
		Err:     err,
	})
	if decision.Fail != nil {
		_ = r.sm.Transition(AgentFailed)
		r.agent.State = AgentFailed
		return nil, decision.Fail
	}
	if decision.Result == nil {
		return nil, nil
	}
	if decision.Result.Status == agentcore.RunYielded {
		_ = r.sm.Transition(AgentSuspended)
		r.agent.State = AgentSuspended
		yieldReason := ""
		if decision.Result.Yield != nil {
			yieldReason = decision.Result.Yield.Reason
		}
		return &TaskResult{
			TaskID:  task.ID,
			Summary: yieldReason,
			Output:  "",
		}, nil
	}
	if decision.Result.Status == agentcore.RunCompleted {
		_ = r.sm.Transition(AgentCompleted)
		r.agent.State = AgentCompleted
		return &TaskResult{
			TaskID:  task.ID,
			Summary: extractSummary(decision.Result.Output),
			Output:  decision.Result.Output,
		}, nil
	}
	return nil, nil
}

func (r *ReactRunner) currentRunState(step int) *agentcore.RunState {
	if r == nil || r.agent == nil {
		return &agentcore.RunState{}
	}
	status := agentcore.RunIdle
	switch r.GetState() {
	case AgentRunning:
		status = agentcore.RunRunning
	case AgentSuspended:
		status = agentcore.RunYielded
	case AgentCompleted:
		status = agentcore.RunCompleted
	case AgentFailed:
		status = agentcore.RunFailed
	}
	state := &agentcore.RunState{
		RunID:   r.agent.ID,
		AgentID: r.agent.ID,
		Status:  status,
		Step:    step,
	}
	if r.agent.Memory != nil {
		msgs := r.agent.Memory.GetMessages()
		state.Messages = make([]agentcore.Message, 0, len(msgs))
		for _, msg := range msgs {
			state.Messages = append(state.Messages, agentcore.Message{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}
	return state
}

func resolveReactResumePolicy(policy agentcore.ResumePolicy) agentcore.ResumePolicy {
	if policy != nil {
		return policy
	}
	return agentcore.DefaultResumePolicy{}
}

func resolveReactToolExecutionPolicy(policy agentcore.ToolExecutionPolicy) agentcore.ToolExecutionPolicy {
	if policy != nil {
		return policy
	}
	return reactDefaultToolExecutionPolicy{}
}

func resolveReactErrorPolicy(policy agentcore.ErrorPolicy) agentcore.ErrorPolicy {
	if policy != nil {
		return policy
	}
	return agentcore.DefaultErrorPolicy{}
}

func toAgentcoreToolCalls(toolCalls []llm.ToolCall) []agentcore.ToolCall {
	if len(toolCalls) == 0 {
		return nil
	}
	out := make([]agentcore.ToolCall, 0, len(toolCalls))
	for _, tc := range toolCalls {
		out = append(out, toAgentcoreToolCall(tc))
	}
	return out
}

func toAgentcoreToolCall(tc llm.ToolCall) agentcore.ToolCall {
	return agentcore.ToolCall{
		CallID:    tc.ID,
		Name:      tc.Name,
		Arguments: tc.Arguments,
	}
}

type reactDefaultToolExecutionPolicy struct{}

func (reactDefaultToolExecutionPolicy) ValidateToolCalls(context.Context, *agentcore.RunState, []agentcore.ToolCall) error {
	return nil
}

func (reactDefaultToolExecutionPolicy) HandleToolError(_ context.Context, _ *agentcore.RunState, call agentcore.ToolCall, err error) agentcore.ToolErrorDecision {
	return agentcore.ToolErrorDecision{
		Result: &agentcore.ToolResult{
			CallID:  call.CallID,
			Name:    call.Name,
			Content: fmt.Sprintf("tool %q error: %s", call.Name, err.Error()),
		},
	}
}

// memToLLMMessages converts memory.Message slice to llm.Message slice.
func memToLLMMessages(msgs []memory.Message) []llm.Message {
	out := make([]llm.Message, len(msgs))
	for i, m := range msgs {
		out[i] = llm.Message{Role: m.Role, Content: m.Content}
	}
	return out
}

// extractSummary returns the first 200 chars of content as a summary.
func extractSummary(content string) string {
	content = strings.TrimSpace(content)
	if len(content) <= 200 {
		return content
	}
	return content[:200] + "…"
}
