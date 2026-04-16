package intrunner

import (
	"context"
	"testing"

	"github.com/gookit/goutil/testutil/assert"

	"github.com/inhere/axon/internal/agent"
	"github.com/inhere/axon/internal/host/session"
	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/internal/platform/tools"
	"github.com/inhere/axon/internal/platform/tracing"
	"github.com/inhere/axon/pkg/agentcore/api"
)

func TestRunner_RunReusesSessionMemoryAcrossTurns(t *testing.T) {
	t.Parallel()

	mock := llm.NewMockProvider("mock").
		QueueText("first answer").
		QueueText("second answer")

	runner, err := New(Config{
		Provider: mock,
		Registry: tools.NewRegistry(),
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "first task",
	})
	assert.NoErr(t, err)

	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_001",
		TurnID:      "turn_002",
		RunID:       "run_002",
		AgentID:     "default",
		Instruction: "second task",
	})
	assert.NoErr(t, err)

	calls := mock.Calls()
	assert.Eq(t, 2, len(calls))
	assert.True(t, containsMessage(calls[1].Messages, "user", "first task"))
	assert.True(t, containsMessage(calls[1].Messages, "assistant", "first answer"))
}

func TestRunner_RunEmitsPreparingAndThinkingStates(t *testing.T) {
	t.Parallel()

	mock := llm.NewMockProvider("mock").QueueText("done")

	runner, err := New(Config{
		Provider: mock,
		Registry: tools.NewRegistry(),
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	var states []string
	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "do something",
		Emit: func(event Event) error {
			if event.EventType != "run.status" {
				return nil
			}
			state, _ := event.Payload["state"].(string)
			states = append(states, state)
			return nil
		},
	})
	assert.NoErr(t, err)
	assert.Eq(t, 2, len(states))
	assert.Eq(t, "PREPARING", states[0])
	assert.Eq(t, "THINKING", states[1])
}

func TestRunner_RunReusesSuspendedRunnerOnContinue(t *testing.T) {
	t.Parallel()

	hook := &yieldBeforeSecondModelHostHook{}
	mock := llm.NewMockProvider("mock").
		QueueToolCall("echo", map[string]any{"msg": "persisted"}).
		QueueText("done after continue")

	registry := tools.NewRegistry()
	registry.MustRegister(&hostEchoTool{})

	runner, err := New(Config{
		Provider: mock,
		Registry: registry,
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
		Hooks:    []agentcore.Hook{hook},
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	first, err := runner.Run(context.Background(), Request{
		SessionID:   "sess_resume",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "first task",
	})
	assert.NoErr(t, err)
	assert.Eq(t, "await_resume", first.Summary)
	assert.Eq(t, session.RunInputRequired, first.WaitState)

	second, err := runner.Run(context.Background(), Request{
		SessionID:   "sess_resume",
		TurnID:      "turn_002",
		RunID:       "run_002",
		AgentID:     "default",
		Instruction: "continue with human input",
	})
	assert.NoErr(t, err)
	assert.Eq(t, "", string(second.WaitState))
	assert.Eq(t, "done after continue", second.Summary)

	calls := mock.Calls()
	assert.Eq(t, 2, len(calls))
	assert.True(t, containsMessage(calls[1].Messages, "tool", "echo: persisted"))
	assert.True(t, containsMessage(calls[1].Messages, "user", "continue with human input"))
}

func TestRunner_RunMapsApprovalYieldToAuthRequired(t *testing.T) {
	t.Parallel()

	hook := &yieldApprovalHostHook{}
	mock := llm.NewMockProvider("mock")

	runner, err := New(Config{
		Provider: mock,
		Registry: tools.NewRegistry(),
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
		Hooks:    []agentcore.Hook{hook},
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	var states []string
	result, err := runner.Run(context.Background(), Request{
		SessionID:   "sess_auth",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "request approval",
		Emit: func(event Event) error {
			if event.EventType != session.EventRunStatus {
				return nil
			}
			state, _ := event.Payload["state"].(string)
			states = append(states, state)
			return nil
		},
	})
	assert.NoErr(t, err)
	assert.Eq(t, "await_approval", result.Summary)
	assert.Eq(t, session.RunAuthRequired, result.WaitState)
	assert.Eq(t, 3, len(states))
	assert.Eq(t, "PREPARING", states[0])
	assert.Eq(t, "THINKING", states[1])
	assert.Eq(t, "AUTH_REQUIRED", states[2])
}

func TestRunner_RuntimeKindDefaultsToAgentcore(t *testing.T) {
	t.Parallel()

	runner, err := New(Config{
		Provider: llm.NewMockProvider("mock"),
		Registry: tools.NewRegistry(),
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	assert.Eq(t, agent.RuntimeAgentcore, runner.runtimeKind())
}

func TestRunner_RuntimeKindAllowsExplicitReactFallback(t *testing.T) {
	t.Parallel()

	runner, err := New(Config{
		Provider: llm.NewMockProvider("mock"),
		Registry: tools.NewRegistry(),
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
		Runtime:  " react ",
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	assert.Eq(t, agent.RuntimeReact, runner.runtimeKind())
}

type yieldBeforeSecondModelHostHook struct {
	agentcore.NopHook
	beforeModelCalls int
}

func (h *yieldBeforeSecondModelHostHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	h.beforeModelCalls++
	if h.beforeModelCalls == 2 {
		ctrl.Yield("await_resume", map[string]any{"step": "after_tool"})
	}
	return nil
}

type yieldApprovalHostHook struct {
	agentcore.NopHook
}

func (h *yieldApprovalHostHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	ctrl.Yield("await_approval", map[string]any{"policy": "manual"})
	return nil
}

type hostEchoTool struct{}

func (e *hostEchoTool) Name() string        { return "echo" }
func (e *hostEchoTool) Description() string { return "echo the msg argument" }
func (e *hostEchoTool) Kind() tools.Kind    { return tools.KindNative }
func (e *hostEchoTool) Schema() api.ToolSchema {
	return api.ToolSchema{Name: "echo"}
}
func (e *hostEchoTool) Execute(_ context.Context, call api.ToolCall) (*api.ToolResult, error) {
	msg, _ := call.Arguments["msg"].(string)
	return &api.ToolResult{
		CallID:  call.CallID,
		Name:    call.Name,
		Content: "echo: " + msg,
	}, nil
}

func containsMessage(messages []api.Message, role, content string) bool {
	for _, msg := range messages {
		if msg.Role == role && msg.Content == content {
			return true
		}
	}
	return false
}
