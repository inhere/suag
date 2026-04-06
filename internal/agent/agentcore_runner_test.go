package agent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/inhere/axon/internal/agent"
	"github.com/inhere/axon/internal/agentcorestorage"
	"github.com/inhere/axon/internal/hitl"
	"github.com/inhere/axon/internal/llm"
	"github.com/inhere/axon/internal/tools"
	"github.com/inhere/axon/internal/tracing"
	"github.com/inhere/axon/pkg/agentcore"
)

type coreEchoTool struct{}

func (e *coreEchoTool) Name() string           { return "echo" }
func (e *coreEchoTool) Description() string    { return "echo the msg argument" }
func (e *coreEchoTool) Kind() tools.Kind       { return tools.KindNative }
func (e *coreEchoTool) Schema() llm.ToolSchema { return llm.ToolSchema{Name: "echo"} }
func (e *coreEchoTool) Execute(_ context.Context, args map[string]any) (string, error) {
	msg, _ := args["msg"].(string)
	return "echo: " + msg, nil
}

func TestAgentcoreRunner_Run_UsesSoulPromptAndSyncsMemory(t *testing.T) {
	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	mock := llm.NewMockProvider("mock").QueueText("hello from agentcore runner")
	a := newTestAgent(mock, newTestRegistry())
	a.Soul = &agent.Soul{Name: "helper", Persona: "You are a precise helper."}

	runner := agent.NewAgentcoreRunner(a, agent.AgentcoreRunnerConfig{
		Storage:  store,
		MaxSteps: 5,
	})

	result, err := runner.Run(context.Background(), newTask("say hello"))
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result.Output != "hello from agentcore runner" {
		t.Fatalf("output = %q, want final answer", result.Output)
	}
	if runner.GetState() != agent.AgentCompleted {
		t.Fatalf("state = %s, want completed", runner.GetState())
	}
	if mock.CallCount() != 1 {
		t.Fatalf("LLM called %d times, want 1", mock.CallCount())
	}

	calls := mock.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	if calls[0].System != "You are a precise helper." {
		t.Fatalf("system prompt = %q, want soul persona", calls[0].System)
	}

	msgs := a.Memory.GetMessages()
	if len(msgs) != 2 {
		t.Fatalf("memory messages = %d, want 2", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "say hello" {
		t.Fatalf("first message = %#v, want user instruction", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "hello from agentcore runner" {
		t.Fatalf("second message = %#v, want assistant answer", msgs[1])
	}
}

func TestAgentcoreRunner_Run_BridgesInternalToolsAndPersistsTranscript(t *testing.T) {
	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	mock := llm.NewMockProvider("mock").
		QueueToolCall("echo", map[string]any{"msg": "hello"}).
		QueueText("done with tool")

	reg := newTestRegistry(&coreEchoTool{})
	a := newTestAgent(mock, reg)

	runner := agent.NewAgentcoreRunner(a, agent.AgentcoreRunnerConfig{
		Storage:  store,
		MaxSteps: 5,
	})

	result, err := runner.Run(context.Background(), newTask("use the echo tool"))
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result.Output != "done with tool" {
		t.Fatalf("output = %q, want final answer", result.Output)
	}
	if mock.CallCount() != 2 {
		t.Fatalf("LLM called %d times, want 2", mock.CallCount())
	}

	msgs := a.Memory.GetMessages()
	if len(msgs) != 3 {
		t.Fatalf("memory messages = %d, want 3", len(msgs))
	}
	if msgs[1].Role != "tool" || msgs[1].Content != "echo: hello" {
		t.Fatalf("tool message = %#v, want tool result", msgs[1])
	}
	if msgs[2].Role != "assistant" || msgs[2].Content != "done with tool" {
		t.Fatalf("assistant message = %#v, want final answer", msgs[2])
	}
}

func TestAgentcoreRunner_Run_EmitsTracingSpans(t *testing.T) {
	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	mock := llm.NewMockProvider("mock").
		QueueToolCall("echo", map[string]any{"msg": "hello"}).
		QueueText("done with tracing")

	reg := newTestRegistry(&coreEchoTool{})
	a := newTestAgent(mock, reg)
	tracer := tracing.NewInMemoryTracer(nil)
	ctx := tracing.WithTrace(context.Background(), tracing.TraceContext{TraceID: "trace-agentcore-runner"})

	runner := agent.NewAgentcoreRunner(a, agent.AgentcoreRunnerConfig{
		Storage:  store,
		MaxSteps: 5,
		Tracer:   tracer,
	})

	result, err := runner.Run(ctx, newTask("trace this task"))
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result.Output != "done with tracing" {
		t.Fatalf("output = %q, want final answer", result.Output)
	}

	spans, err := tracer.GetTrace("trace-agentcore-runner")
	if err != nil {
		t.Fatalf("GetTrace() error: %v", err)
	}
	if len(spans) < 4 {
		t.Fatalf("spans = %d, want at least task + llm + tool + llm", len(spans))
	}

	var taskCount, llmCount, toolCount int
	for _, span := range spans {
		switch span.Operation {
		case "task":
			taskCount++
		case "llm_call":
			llmCount++
		case "tool_call":
			toolCount++
		}
	}
	if taskCount != 1 {
		t.Fatalf("task spans = %d, want 1", taskCount)
	}
	if llmCount != 2 {
		t.Fatalf("llm spans = %d, want 2", llmCount)
	}
	if toolCount != 1 {
		t.Fatalf("tool spans = %d, want 1", toolCount)
	}
}

func TestAgentcoreRunner_Run_TriggersHITLOnConsecutiveToolFailures(t *testing.T) {
	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	ft := &failTool{}
	mock := llm.NewMockProvider("mock").
		QueueToolCall("fail_tool", nil).
		QueueToolCall("fail_tool", nil).
		QueueToolCall("fail_tool", nil).
		QueueText("Recovered after human intervention.")

	a := newTestAgent(mock, newTestRegistry(ft))
	hitlMgr := hitl.NewInMemoryManager(nil)
	runner := agent.NewAgentcoreRunner(a, agent.AgentcoreRunnerConfig{
		Storage:       store,
		MaxSteps:      6,
		HITL:          hitlMgr,
		FailThreshold: 3,
	})

	type runResult struct {
		result *agent.TaskResult
		err    error
	}
	ch := make(chan runResult, 1)
	go func() {
		r, runErr := runner.Run(context.Background(), newTask("Use fail_tool"))
		ch <- runResult{result: r, err: runErr}
	}()

	var ticket *hitl.Ticket
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pending := hitlMgr.ListPending()
		if len(pending) > 0 {
			ticket = pending[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ticket == nil {
		t.Fatal("HITL ticket was never created")
	}

	if err := hitlMgr.Respond(ticket.ID, "Stop using fail_tool, just finish."); err != nil {
		t.Fatalf("Respond error: %v", err)
	}

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("Run() error after HITL: %v", r.err)
		}
		if r.result.Output != "Recovered after human intervention." {
			t.Fatalf("output = %q", r.result.Output)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for agent to complete after HITL")
	}
}

func TestAgentcoreRunner_Run_HITLDismissed(t *testing.T) {
	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	mock := llm.NewMockProvider("mock").
		QueueToolCall("fail_tool", nil).
		QueueToolCall("fail_tool", nil).
		QueueToolCall("fail_tool", nil)

	a := newTestAgent(mock, newTestRegistry(&failTool{}))
	hitlMgr := hitl.NewInMemoryManager(nil)
	runner := agent.NewAgentcoreRunner(a, agent.AgentcoreRunnerConfig{
		Storage:       store,
		MaxSteps:      6,
		HITL:          hitlMgr,
		FailThreshold: 3,
	})

	ch := make(chan error, 1)
	go func() {
		_, runErr := runner.Run(context.Background(), newTask("fail"))
		ch <- runErr
	}()

	var ticket *hitl.Ticket
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if p := hitlMgr.ListPending(); len(p) > 0 {
			ticket = p[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ticket == nil {
		t.Fatal("no HITL ticket")
	}

	if err := hitlMgr.Dismiss(ticket.ID, "task is unrecoverable"); err != nil {
		t.Fatalf("Dismiss error: %v", err)
	}

	select {
	case runErr := <-ch:
		if runErr == nil {
			t.Fatal("expected error after HITL dismiss, got nil")
		}
		if runner.GetState() != agent.AgentFailed {
			t.Fatalf("state = %s, want failed", runner.GetState())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}

func TestAgentcoreRunner_Run_MaxStepsExceeded(t *testing.T) {
	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	mock := llm.NewMockProvider("mock")
	for range 10 {
		mock.QueueToolCall("echo", map[string]any{"msg": "loop"})
	}

	a := newTestAgent(mock, newTestRegistry(&coreEchoTool{}))
	runner := agent.NewAgentcoreRunner(a, agent.AgentcoreRunnerConfig{
		Storage:  store,
		MaxSteps: 3,
	})

	_, err = runner.Run(context.Background(), newTask("loop forever"))
	if err == nil {
		t.Fatal("expected max-steps error, got nil")
	}
	if runner.GetState() != agent.AgentFailed {
		t.Fatalf("state = %s, want failed", runner.GetState())
	}
}

func TestAgentcoreRunner_Run_ContextCancelled(t *testing.T) {
	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	mock := llm.NewMockProvider("manual")
	a := newTestAgent(mock, newTestRegistry())
	runner := agent.NewAgentcoreRunner(a, agent.AgentcoreRunnerConfig{
		Storage:  store,
		MaxSteps: 5,
	})

	ctx, cancel := context.WithCancel(context.Background())

	ch := make(chan error, 1)
	go func() {
		_, runErr := runner.Run(ctx, newTask("will be cancelled"))
		ch <- runErr
	}()

	cancel()

	select {
	case runErr := <-ch:
		if runErr == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for cancellation")
	}
}

func TestAgentcoreRunner_Run_ManualStepThrough(t *testing.T) {
	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	mock := llm.NewMockProvider("manual").
		QueueToolCall("echo", map[string]any{"msg": "ping"})
	a := newTestAgent(mock, newTestRegistry(&coreEchoTool{}))
	runner := agent.NewAgentcoreRunner(a, agent.AgentcoreRunnerConfig{
		Storage:  store,
		MaxSteps: 5,
	})

	ctx := context.Background()
	type runResult struct {
		result *agent.TaskResult
		err    error
	}
	ch := make(chan runResult, 1)
	go func() {
		r, runErr := runner.Run(ctx, newTask("manual step-through test"))
		ch <- runResult{result: r, err: runErr}
	}()

	req1, reply1, err := mock.WaitForCall(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(req1.Messages) == 0 {
		t.Fatal("expected at least one message in first request")
	}
	reply1(llm.MockResponse{
		ToolCalls: []llm.ToolCall{{
			ID:        "c1",
			Name:      "echo",
			Arguments: map[string]any{"msg": "ping"},
		}},
	})

	req2, reply2, err := mock.WaitForCall(ctx)
	if err != nil {
		t.Fatal(err)
	}
	foundToolMsg := false
	for _, m := range req2.Messages {
		if m.Role == "tool" && m.Content == "echo: ping" {
			foundToolMsg = true
		}
	}
	if !foundToolMsg {
		t.Fatalf("tool result missing from second LLM call messages: %+v", req2.Messages)
	}
	reply2(llm.MockResponse{Content: "ping received, done"})

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("Run() error: %v", r.err)
		}
		if r.result.Output != "ping received, done" {
			t.Fatalf("output = %q", r.result.Output)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}

type yieldBeforeSecondModelAgentHook struct {
	agentcore.NopHook
	beforeModelCalls int
}

func (h *yieldBeforeSecondModelAgentHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	h.beforeModelCalls++
	if h.beforeModelCalls == 2 {
		ctrl.Yield("await_resume", map[string]any{"step": "after_tool"})
	}
	return nil
}

func TestAgentcoreRunner_Run_CanResumeYieldedRunFromState(t *testing.T) {
	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	hook := &yieldBeforeSecondModelAgentHook{}
	mock := llm.NewMockProvider("mock").
		QueueToolCall("echo", map[string]any{"msg": "persisted"}).
		QueueText("completed after resume")

	a := newTestAgent(mock, newTestRegistry(&coreEchoTool{}))
	runner := agent.NewAgentcoreRunner(a, agent.AgentcoreRunnerConfig{
		Storage:  store,
		MaxSteps: 5,
		Hooks:    []agentcore.Hook{hook},
	})

	firstResult, err := runner.Run(context.Background(), newTask("resume from state"))
	if err != nil {
		t.Fatalf("first Run() error: %v", err)
	}
	if runner.GetState() != agent.AgentSuspended {
		t.Fatalf("state after first run = %s, want suspended", runner.GetState())
	}
	if firstResult.Summary != "await_resume" {
		t.Fatalf("summary = %q, want yield reason", firstResult.Summary)
	}

	secondResult, err := runner.Run(context.Background(), newTask("resume from state"))
	if err != nil {
		t.Fatalf("second Run() error: %v", err)
	}
	if secondResult.Output != "completed after resume" {
		t.Fatalf("output = %q, want resumed completion", secondResult.Output)
	}
	if runner.GetState() != agent.AgentCompleted {
		t.Fatalf("state after resume = %s, want completed", runner.GetState())
	}
	if mock.CallCount() != 2 {
		t.Fatalf("LLM called %d times, want 2 total across yield + resume", mock.CallCount())
	}
}

func TestAgentcoreRunner_Resume_UsesInstructionOnNextRun(t *testing.T) {
	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	hook := &yieldBeforeSecondModelAgentHook{}
	mock := llm.NewMockProvider("manual").
		QueueToolCall("echo", map[string]any{"msg": "ping"})
	a := newTestAgent(mock, newTestRegistry(&coreEchoTool{}))
	runner := agent.NewAgentcoreRunner(a, agent.AgentcoreRunnerConfig{
		Storage:  store,
		MaxSteps: 5,
		Hooks:    []agentcore.Hook{hook},
	})

	firstResult, err := runner.Run(context.Background(), newTask("resume with instruction"))
	if err != nil {
		t.Fatalf("first Run() error: %v", err)
	}
	if firstResult.Summary != "await_resume" {
		t.Fatalf("first result summary = %q, want yield reason", firstResult.Summary)
	}
	if runner.GetState() != agent.AgentSuspended {
		t.Fatalf("state after yield = %s, want suspended", runner.GetState())
	}

	if err := runner.Resume("Please finish using the persisted tool result."); err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	secondDone := make(chan parityRunOutcome, 1)
	go func() {
		r, runErr := runner.Run(context.Background(), newTask("resume with instruction"))
		secondDone <- parityRunOutcome{result: r, err: runErr}
	}()

	req2, reply2, err := mock.WaitForCall(context.Background())
	if err != nil {
		t.Fatalf("second WaitForCall error: %v", err)
	}
	foundHumanInstruction := false
	for _, msg := range req2.Messages {
		if msg.Role == "user" && msg.Content == "Please finish using the persisted tool result." {
			foundHumanInstruction = true
			break
		}
	}
	if !foundHumanInstruction {
		t.Fatalf("second request messages = %#v, want resume instruction in model input", req2.Messages)
	}
	reply2(llm.MockResponse{Content: "done after instruction"})

	outcome := waitAgentcoreOutcome(t, secondDone)
	if outcome.err != nil {
		t.Fatalf("second Run() error: %v", outcome.err)
	}
	if outcome.result.Output != "done after instruction" {
		t.Fatalf("output = %q, want resumed completion", outcome.result.Output)
	}
}
