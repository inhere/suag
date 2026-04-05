package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/inhere/axon/internal/hitl"
	"github.com/inhere/axon/internal/llm"
	"github.com/inhere/axon/internal/memory"
	"github.com/inhere/axon/internal/tracing"
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
	agent      *Agent
	sm         *stateMachine
	maxSteps   int
	failThresh int
	tracer     tracing.Tracer
	hitlMgr    hitl.Manager
}

// NewReactRunner creates a legacy ReactRunner for the given agent.
//
// Deprecated: prefer NewAgentcoreRunner for new integrations. Keep this
// constructor only for explicit fallback wiring and parity tests.
func NewReactRunner(a *Agent, maxSteps int, tracer tracing.Tracer, hitlMgr hitl.Manager) *ReactRunner {
	if maxSteps <= 0 {
		maxSteps = defaultMaxSteps
	}
	return &ReactRunner{
		agent:      a,
		sm:         newStateMachine(a.State),
		maxSteps:   maxSteps,
		failThresh: defaultFailThresh,
		tracer:     tracer,
		hitlMgr:    hitlMgr,
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
			return nil, fmt.Errorf("llm call step %d: %w", step, llmErr)
		}
		llmSpan.Output = llmResp.Content
		r.tracer.FinishSpan(llmSpan, nil)

		// ── Tool call branch ──────────────────────────────────────────────────
		if llmResp.IsToolCall() {
			for _, tc := range llmResp.ToolCalls {
				result, toolErr := r.executeToolCall(ctx, tc)
				if toolErr != nil {
					consecutiveFails++
					slog.Warn("tool call failed",
						"agent", r.agent.ID,
						"tool", tc.Name,
						"err", toolErr,
						"consecutive", consecutiveFails,
					)

					errMsg := fmt.Sprintf("tool %q error: %s", tc.Name, toolErr.Error())
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
	if err := r.sm.Transition(AgentRunning); err != nil {
		return err
	}
	r.agent.State = AgentRunning
	r.agent.Memory.AddMessage("system", "[Human instruction]: "+instruction)
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
