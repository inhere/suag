package intrunner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gookit/goutil/testutil/assert"

	"github.com/inhere/axon/internal/agent"
	"github.com/inhere/axon/internal/host/session"
	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/internal/platform/tools"
	"github.com/inhere/axon/internal/platform/tracing"
	"github.com/inhere/axon/pkg/agentcore"
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

func TestRunner_RunInjectsFormalMemoryIntoProviderContext(t *testing.T) {
	t.Parallel()

	memoryRoot := t.TempDir()
	err := os.MkdirAll(filepath.Join(memoryRoot, "memory"), 0o755)
	assert.NoErr(t, err)
	err = os.WriteFile(filepath.Join(memoryRoot, "memory", "MEMORY.md"), []byte("# MEMORY\n\nUpdated: 2026-04-20T00:00:00Z\n\n- [turn_learning] prefers concise execution notes\n"), 0o644)
	assert.NoErr(t, err)
	err = os.WriteFile(filepath.Join(memoryRoot, "memory", "USER.md"), []byte("# USER\n\nUpdated: 2026-04-20T00:00:00Z\n\n- [user_preference] 优先中文回复\n"), 0o644)
	assert.NoErr(t, err)

	mock := llm.NewMockProvider("mock").QueueText("done")

	runner, err := New(Config{
		Provider:   mock,
		Registry:   tools.NewRegistry(),
		Tracer:     tracing.NewInMemoryTracer(nil),
		RunRoot:    t.TempDir(),
		MemoryRoot: memoryRoot,
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_memory",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "say hi",
	})
	assert.NoErr(t, err)

	calls := mock.Calls()
	assert.Eq(t, 1, len(calls))
	assert.True(t, containsMessageContains(calls[0].Messages, "system", "[Host MEMORY]\n- [turn_learning] prefers concise execution notes"))
	assert.True(t, containsMessageContains(calls[0].Messages, "system", "[Host USER]\n- [user_preference] 优先中文回复"))
}

func TestRunner_RunSkipsMissingFormalMemoryFiles(t *testing.T) {
	t.Parallel()

	mock := llm.NewMockProvider("mock").QueueText("done")

	runner, err := New(Config{
		Provider:   mock,
		Registry:   tools.NewRegistry(),
		Tracer:     tracing.NewInMemoryTracer(nil),
		RunRoot:    t.TempDir(),
		MemoryRoot: t.TempDir(),
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_no_memory",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "say hi",
	})
	assert.NoErr(t, err)

	calls := mock.Calls()
	assert.Eq(t, 1, len(calls))
	assert.False(t, containsMessageContains(calls[0].Messages, "system", "[Host MEMORY]"))
	assert.False(t, containsMessageContains(calls[0].Messages, "system", "[Host USER]"))
}

func TestRunner_RunRefreshesFormalMemoryAcrossTurns(t *testing.T) {
	t.Parallel()

	memoryRoot := t.TempDir()
	err := os.MkdirAll(filepath.Join(memoryRoot, "memory"), 0o755)
	assert.NoErr(t, err)

	writeFormal := func(content string) {
		err := os.WriteFile(filepath.Join(memoryRoot, "memory", "MEMORY.md"), []byte(content), 0o644)
		assert.NoErr(t, err)
	}
	writeFormal("# MEMORY\n\nUpdated: 2026-04-20T00:00:00Z\n\n- [turn_learning] first memory\n")

	mock := llm.NewMockProvider("mock").
		QueueText("first answer").
		QueueText("second answer")

	runner, err := New(Config{
		Provider:   mock,
		Registry:   tools.NewRegistry(),
		Tracer:     tracing.NewInMemoryTracer(nil),
		RunRoot:    t.TempDir(),
		MemoryRoot: memoryRoot,
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_refresh",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "first task",
	})
	assert.NoErr(t, err)

	writeFormal("# MEMORY\n\nUpdated: 2026-04-20T00:01:00Z\n\n- [turn_learning] second memory\n")

	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_refresh",
		TurnID:      "turn_002",
		RunID:       "run_002",
		AgentID:     "default",
		Instruction: "second task",
	})
	assert.NoErr(t, err)

	calls := mock.Calls()
	assert.Eq(t, 2, len(calls))
	assert.True(t, containsMessageContains(calls[0].Messages, "system", "first memory"))
	assert.True(t, containsMessageContains(calls[1].Messages, "system", "second memory"))
	assert.False(t, containsMessageContains(calls[1].Messages, "system", "first memory"))
}

func TestRunner_RunReturnsFormalMemoryRecallMetadata(t *testing.T) {
	t.Parallel()

	memoryRoot := t.TempDir()
	err := os.MkdirAll(filepath.Join(memoryRoot, "memory"), 0o755)
	assert.NoErr(t, err)
	err = os.WriteFile(
		filepath.Join(memoryRoot, "memory", "MEMORY.md"),
		[]byte("# MEMORY\n\nUpdated: 2026-04-20T00:00:00Z\n\n- [turn_learning] prefer concise notes\n- [turn_learning] capture next step\n"),
		0o644,
	)
	assert.NoErr(t, err)
	err = os.WriteFile(
		filepath.Join(memoryRoot, "memory", "USER.md"),
		[]byte("# USER\n\nUpdated: 2026-04-20T00:00:00Z\n\n- [user_preference] 优先中文回复\n"),
		0o644,
	)
	assert.NoErr(t, err)

	mock := llm.NewMockProvider("mock").QueueText("done")

	runner, err := New(Config{
		Provider:   mock,
		Registry:   tools.NewRegistry(),
		Tracer:     tracing.NewInMemoryTracer(nil),
		RunRoot:    t.TempDir(),
		MemoryRoot: memoryRoot,
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	result, err := runner.Run(context.Background(), Request{
		SessionID:   "sess_recall_meta",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "say hi",
	})
	assert.NoErr(t, err)

	recall, ok := result.Metadata["memory_recall"].(map[string]any)
	if !ok {
		t.Fatalf("Run().Metadata[memory_recall] type = %T, want map[string]any", result.Metadata["memory_recall"])
	}
	assert.Eq(t, true, recall["enabled"])
	assert.Eq(t, 3, recall["entry_count"])
	sources, ok := recall["sources"].([]map[string]any)
	if !ok {
		t.Fatalf("Run().Metadata[memory_recall][sources] type = %T, want []map[string]any", recall["sources"])
	}
	assert.Eq(t, 2, len(sources))
	assert.Eq(t, "MEMORY", sources[0]["name"])
	assert.Eq(t, 2, sources[0]["entry_count"])
	assert.Eq(t, "USER", sources[1]["name"])
	assert.Eq(t, 1, sources[1]["entry_count"])
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

func TestRunner_RunAddsInitialLoopMetadataToYieldedWait(t *testing.T) {
	t.Parallel()

	hook := &yieldOnFirstModelHostHook{}
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

	var statusPayloads []map[string]any
	result, err := runner.Run(context.Background(), Request{
		SessionID:   "sess_initial_wait",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "pause immediately",
		Emit: func(event Event) error {
			if event.EventType != session.EventRunStatus {
				return nil
			}
			payload := map[string]any{}
			for key, value := range event.Payload {
				payload[key] = value
			}
			statusPayloads = append(statusPayloads, payload)
			return nil
		},
	})
	assert.NoErr(t, err)
	assert.Eq(t, session.RunInputRequired, result.WaitState)
	assert.Eq(t, "initial", result.WaitMetadata["loop_phase"])
	assert.Eq(t, 0, result.WaitMetadata["resume_count"])
	if payload, ok := statusPayloads[2]["waitMetadata"].(map[string]any); ok {
		assert.Eq(t, "initial", payload["loop_phase"])
		assert.Eq(t, 0, payload["resume_count"])
	} else {
		t.Fatalf("status payload waitMetadata type = %T, want map[string]any", statusPayloads[2]["waitMetadata"])
	}
}

func TestRunner_RunAddsResumedLoopMetadataForResumeModes(t *testing.T) {
	t.Parallel()

	t.Run("resume with input", func(t *testing.T) {
		hook := &yieldOnFirstTwoModelsHostHook{}
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

		_, err = runner.Run(context.Background(), Request{
			SessionID:   "sess_resume_wait_input",
			TurnID:      "turn_001",
			RunID:       "run_001",
			AgentID:     "default",
			Instruction: "first wait",
		})
		assert.NoErr(t, err)

		result, err := runner.Run(context.Background(), Request{
			SessionID:   "sess_resume_wait_input",
			TurnID:      "turn_002",
			RunID:       "run_002",
			AgentID:     "default",
			Instruction: "resume with message",
		})
		assert.NoErr(t, err)
		assert.Eq(t, session.RunInputRequired, result.WaitState)
		assert.Eq(t, "resumed", result.WaitMetadata["loop_phase"])
		assert.Eq(t, "resume_with_input", result.WaitMetadata["resume_source"])
		assert.Eq(t, 1, result.WaitMetadata["resume_count"])
	})

	t.Run("resume from state", func(t *testing.T) {
		hook := &yieldOnFirstTwoModelsHostHook{}
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

		_, err = runner.Run(context.Background(), Request{
			SessionID:   "sess_resume_wait_state",
			TurnID:      "turn_001",
			RunID:       "run_001",
			AgentID:     "default",
			Instruction: "first wait",
		})
		assert.NoErr(t, err)

		result, err := runner.Run(context.Background(), Request{
			SessionID: "sess_resume_wait_state",
			TurnID:    "turn_002",
			RunID:     "run_002",
			AgentID:   "default",
		})
		assert.NoErr(t, err)
		assert.Eq(t, session.RunInputRequired, result.WaitState)
		assert.Eq(t, "resumed", result.WaitMetadata["loop_phase"])
		assert.Eq(t, "resume_from_state", result.WaitMetadata["resume_source"])
		assert.Eq(t, 1, result.WaitMetadata["resume_count"])
	})
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
	var statusPayloads []map[string]any
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
			payload := map[string]any{}
			for key, value := range event.Payload {
				payload[key] = value
			}
			statusPayloads = append(statusPayloads, payload)
			return nil
		},
	})
	assert.NoErr(t, err)
	assert.Eq(t, "await_approval", result.Summary)
	assert.Eq(t, session.RunAuthRequired, result.WaitState)
	assert.Eq(t, "manual", result.WaitMetadata["policy"])
	if payload, ok := statusPayloads[2]["waitMetadata"].(map[string]any); ok {
		assert.Eq(t, "manual", payload["policy"])
	} else {
		t.Fatalf("states[2].Payload[waitMetadata] type = %T, want map[string]any", statusPayloads[2]["waitMetadata"])
	}
	assert.Eq(t, 3, len(states))
	assert.Eq(t, "PREPARING", states[0])
	assert.Eq(t, "THINKING", states[1])
	assert.Eq(t, "AUTH_REQUIRED", states[2])
}

func TestRunner_RunUsesStructuredWaitKindBeforeSummaryFallback(t *testing.T) {
	t.Parallel()

	hook := &yieldStructuredInputHostHook{}
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

	result, err := runner.Run(context.Background(), Request{
		SessionID:   "sess_wait_kind",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "needs more info",
	})
	assert.NoErr(t, err)
	assert.Eq(t, "await_review", result.Summary)
	assert.Eq(t, session.RunAuthRequired, result.WaitState)
}

func TestRunner_RunKeepsSummaryFallbackWhenWaitKindMissing(t *testing.T) {
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

	result, err := runner.Run(context.Background(), Request{
		SessionID:   "sess_wait_fallback",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "request approval",
	})
	assert.NoErr(t, err)
	assert.Eq(t, "await_approval", result.Summary)
	assert.Eq(t, session.RunAuthRequired, result.WaitState)
}

func TestRunner_ValidateContinueRejectsWhenSuspendedRuntimeIsMissing(t *testing.T) {
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

	err = runner.ValidateContinue(context.Background(), Request{
		SessionID: "sess_resume_missing",
		Context: map[string]any{
			"resume_turn_id":     "turn_001",
			"resume_from_run_id": "run_001",
		},
	})
	assert.Err(t, err)
	assert.Eq(t, "waiting runtime is no longer available; start a new turn or reissue the task", err.Error())
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

type yieldStructuredInputHostHook struct {
	agentcore.NopHook
}

type yieldOnFirstModelHostHook struct {
	agentcore.NopHook
	beforeModelCalls int
}

func (h *yieldOnFirstModelHostHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	h.beforeModelCalls++
	if h.beforeModelCalls == 1 {
		ctrl.Yield("await_resume", map[string]any{"wait_kind": "input_required"})
	}
	return nil
}

type yieldOnFirstTwoModelsHostHook struct {
	agentcore.NopHook
	beforeModelCalls int
}

func (h *yieldOnFirstTwoModelsHostHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	h.beforeModelCalls++
	if h.beforeModelCalls == 1 || h.beforeModelCalls == 2 {
		ctrl.Yield("await_resume", map[string]any{"wait_kind": "input_required"})
	}
	return nil
}

func (h *yieldStructuredInputHostHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	ctrl.Yield("await_review", map[string]any{
		"wait_kind": "auth_required",
		"policy":    "manual_review",
	})
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

func containsMessageContains(messages []api.Message, role, content string) bool {
	for _, msg := range messages {
		if msg.Role == role && strings.Contains(msg.Content, content) {
			return true
		}
	}
	return false
}
