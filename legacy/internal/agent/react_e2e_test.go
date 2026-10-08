package agent_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/inhere/suag/internal/agent"
	"github.com/inhere/suag/internal/ops/hitl"
	"github.com/inhere/suag/internal/platform/llm"
	"github.com/inhere/suag/internal/platform/memory"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/internal/platform/tracing"
	"github.com/inhere/suag/pkg/agentcore"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// echoTool returns its "msg" argument as output. Used to simulate successful tool calls.
type echoTool struct{}

func (e *echoTool) Name() string                 { return "echo" }
func (e *echoTool) Description() string          { return "echo the msg argument" }
func (e *echoTool) Kind() tools.Kind             { return tools.KindNative }
func (e *echoTool) Schema() agentcore.ToolSchema { return agentcore.ToolSchema{Name: "echo"} }
func (e *echoTool) Execute(_ context.Context, call agentcore.ToolCall) (*agentcore.ToolResult, error) {
	msg, _ := call.Arguments["msg"].(string)
	return &agentcore.ToolResult{
		CallID:  call.CallID,
		Name:    call.Name,
		Content: "echo: " + msg,
	}, nil
}

// failTool always returns an error. Used to trigger HITL.
type failTool struct{ callCount int }

func (f *failTool) Name() string                 { return "fail_tool" }
func (f *failTool) Description() string          { return "always fails" }
func (f *failTool) Kind() tools.Kind             { return tools.KindNative }
func (f *failTool) Schema() agentcore.ToolSchema { return agentcore.ToolSchema{Name: "fail_tool"} }
func (f *failTool) Execute(_ context.Context, _ agentcore.ToolCall) (*agentcore.ToolResult, error) {
	f.callCount++
	return nil, fmt.Errorf("tool error #%d", f.callCount)
}

func newTestRegistry(ts ...tools.Tool) *tools.Registry {
	reg := tools.NewRegistry()
	for _, t := range ts {
		reg.MustRegister(t)
	}
	return reg
}

func newTestAgent(mock llm.Provider, reg *tools.Registry) *agent.Agent {
	mem := memory.NewInMemStore(memory.InMemConfig{})
	a, _ := agent.New(agent.Config{
		Name:   "test-agent",
		LLM:    mock,
		Memory: mem,
		Tools:  reg,
	})
	return a
}

func newRunner(a *agent.Agent, tracer tracing.Tracer, hitlMgr hitl.Manager) *agent.ReactRunner {
	return agent.NewReactRunner(a, 20, tracer, hitlMgr)
}

func newTask(desc string) *agent.Task {
	return &agent.Task{
		ID:          "task-e2e",
		Title:       "e2e test task",
		Description: desc,
		CreatedAt:   time.Now(),
	}
}

// ─── Test 1: direct answer (no tool calls) ────────────────────────────────────

func TestReactLoop_DirectAnswer(t *testing.T) {
	mock := llm.NewMockProvider("mock").
		QueueText("The answer is 42.")

	a := newTestAgent(mock, newTestRegistry())
	tracer := tracing.NewInMemoryTracer(nil)
	runner := newRunner(a, tracer, nil)

	result, err := runner.Run(context.Background(), newTask("What is the answer?"))
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result.Output != "The answer is 42." {
		t.Errorf("output = %q", result.Output)
	}
	if mock.CallCount() != 1 {
		t.Errorf("LLM called %d times, want 1", mock.CallCount())
	}
	if runner.GetState() != agent.AgentCompleted {
		t.Errorf("state = %s, want completed", runner.GetState())
	}
}

// ─── Test 2: one tool call then answer ────────────────────────────────────────

func TestReactLoop_OneToolCall(t *testing.T) {
	mock := llm.NewMockProvider("mock").
		QueueToolCall("echo", map[string]any{"msg": "hello"}).
		QueueText("Tool returned echo: hello. Task done.")

	reg := newTestRegistry(&echoTool{})
	a := newTestAgent(mock, reg)
	tracer := tracing.NewInMemoryTracer(nil)
	runner := newRunner(a, tracer, nil)

	result, err := runner.Run(context.Background(), newTask("Echo hello then finish"))
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if mock.CallCount() != 2 {
		t.Errorf("LLM called %d times, want 2", mock.CallCount())
	}

	// Second LLM call must contain the tool result in messages.
	calls := mock.Calls()
	lastMsgs := calls[1].Messages
	foundToolResult := false
	for _, m := range lastMsgs {
		if m.Role == "tool" && m.Content == "echo: hello" {
			foundToolResult = true
		}
	}
	if !foundToolResult {
		t.Errorf("tool result not found in second LLM request messages: %+v", lastMsgs)
	}
	if result.Output == "" {
		t.Error("expected non-empty output")
	}
}

// ─── Test 3: multi-step tool chain ────────────────────────────────────────────

func TestReactLoop_MultiStepToolChain(t *testing.T) {
	mock := llm.NewMockProvider("mock").
		QueueToolCall("echo", map[string]any{"msg": "step1"}).
		QueueToolCall("echo", map[string]any{"msg": "step2"}).
		QueueToolCall("echo", map[string]any{"msg": "step3"}).
		QueueText("All three steps done.")

	reg := newTestRegistry(&echoTool{})
	a := newTestAgent(mock, reg)
	runner := newRunner(a, tracing.NewInMemoryTracer(nil), nil)

	result, err := runner.Run(context.Background(), newTask("Run three echo steps"))
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if mock.CallCount() != 4 {
		t.Errorf("LLM called %d times, want 4", mock.CallCount())
	}
	if result.Output != "All three steps done." {
		t.Errorf("output = %q", result.Output)
	}
}

// ─── Test 4: HITL triggered after 3 consecutive tool failures ─────────────────

func TestReactLoop_HITLTriggeredOnConsecutiveFailures(t *testing.T) {
	ft := &failTool{}
	mock := llm.NewMockProvider("mock").
		// LLM repeatedly requests the failing tool (3 times → HITL).
		QueueToolCall("fail_tool", nil).
		QueueToolCall("fail_tool", nil).
		QueueToolCall("fail_tool", nil).
		// After HITL resume: LLM produces final answer.
		QueueText("Recovered after human intervention.")

	reg := newTestRegistry(ft)
	a := newTestAgent(mock, reg)
	tracer := tracing.NewInMemoryTracer(nil)

	hitlMgr := hitl.NewInMemoryManager(nil)
	runner := newRunner(a, tracer, hitlMgr)

	ctx := context.Background()

	// Run the agent in a goroutine — it will block at HITL.
	type runResult struct {
		result *agent.TaskResult
		err    error
	}
	ch := make(chan runResult, 1)
	go func() {
		r, err := runner.Run(ctx, newTask("Use fail_tool"))
		ch <- runResult{r, err}
	}()

	// Wait for the HITL ticket to appear (poll with timeout).
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
	if ticket.AgentID != a.ID {
		t.Errorf("ticket.AgentID = %q, want %q", ticket.AgentID, a.ID)
	}

	// Human operator responds: resume with corrective instruction.
	if err := hitlMgr.Respond(ticket.ID, "Stop using fail_tool, just finish."); err != nil {
		t.Fatalf("Respond error: %v", err)
	}

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("Run() error after HITL: %v", r.err)
		}
		if r.result.Output != "Recovered after human intervention." {
			t.Errorf("output = %q", r.result.Output)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for agent to complete after HITL")
	}

	if ft.callCount != 3 {
		t.Errorf("fail_tool called %d times, want 3", ft.callCount)
	}
}

// ─── Test 5: HITL dismissed → agent fails ─────────────────────────────────────

func TestReactLoop_HITLDismissed(t *testing.T) {
	mock := llm.NewMockProvider("mock").
		QueueToolCall("fail_tool", nil).
		QueueToolCall("fail_tool", nil).
		QueueToolCall("fail_tool", nil)

	reg := newTestRegistry(&failTool{})
	hitlMgr := hitl.NewInMemoryManager(nil)
	runner := newRunner(newTestAgent(mock, reg), tracing.NewInMemoryTracer(nil), hitlMgr)

	ctx := context.Background()
	ch := make(chan error, 1)
	go func() {
		_, err := runner.Run(ctx, newTask("fail"))
		ch <- err
	}()

	// Wait for ticket.
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

	// Operator dismisses → agent should fail.
	_ = hitlMgr.Dismiss(ticket.ID, "task is unrecoverable")

	select {
	case err := <-ch:
		if err == nil {
			t.Fatal("expected error after HITL dismiss, got nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}

// ─── Test 6: max steps exceeded ───────────────────────────────────────────────

func TestReactLoop_MaxStepsExceeded(t *testing.T) {
	// Agent keeps requesting tools — never sends a final text answer.
	mock := llm.NewMockProvider("mock")
	for range 10 {
		mock.QueueToolCall("echo", map[string]any{"msg": "loop"})
	}

	reg := newTestRegistry(&echoTool{})
	a := newTestAgent(mock, reg)
	// Cap at 3 steps to force early termination.
	runner := agent.NewReactRunner(a, 3, tracing.NewInMemoryTracer(nil), nil)

	_, err := runner.Run(context.Background(), newTask("loop forever"))
	if err == nil {
		t.Fatal("expected max-steps error, got nil")
	}
	if runner.GetState() != agent.AgentFailed {
		t.Errorf("state = %s, want failed", runner.GetState())
	}
}

// ─── Test 7: context cancellation ────────────────────────────────────────────

func TestReactLoop_ContextCancelled(t *testing.T) {
	mock := llm.NewMockProvider("manual") // manual → blocks until ctx cancelled
	a := newTestAgent(mock, newTestRegistry())
	runner := newRunner(a, tracing.NewInMemoryTracer(nil), nil)

	ctx, cancel := context.WithCancel(context.Background())

	ch := make(chan error, 1)
	go func() {
		_, err := runner.Run(ctx, newTask("will be cancelled"))
		ch <- err
	}()

	// Cancel while agent is blocked waiting for LLM.
	cancel()

	select {
	case err := <-ch:
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for cancellation")
	}
}

// ─── Test 8: manual mode step-through ────────────────────────────────────────

func TestReactLoop_ManualStepThrough(t *testing.T) {
	mock := llm.NewMockProvider("manual")
	reg := newTestRegistry(&echoTool{})
	a := newTestAgent(mock, reg)
	runner := newRunner(a, tracing.NewInMemoryTracer(nil), nil)
	ctx := context.Background()

	type runResult struct {
		result *agent.TaskResult
		err    error
	}
	ch := make(chan runResult, 1)
	go func() {
		r, err := runner.Run(ctx, newTask("manual step-through test"))
		ch <- runResult{r, err}
	}()

	// Step 1: agent asks LLM → we inject a tool call.
	req1, reply1, err := mock.WaitForCall(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(req1.Messages) == 0 {
		t.Error("expected at least one message in first request")
	}
	reply1(llm.MockResponse{
		ToolCalls: []agentcore.ToolCall{{
			CallID: "c1",
			Name:   "echo",
			Arguments: map[string]any{
				"msg": "ping",
			},
		}},
	})

	// Step 2: tool ran (echo: ping); agent asks LLM again → inject final answer.
	req2, reply2, err := mock.WaitForCall(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Verify tool result was injected into the conversation.
	foundToolMsg := false
	for _, m := range req2.Messages {
		if m.Role == "tool" && m.Content == "echo: ping" {
			foundToolMsg = true
		}
	}
	if !foundToolMsg {
		t.Errorf("tool result missing from second LLM call messages: %+v", req2.Messages)
	}
	reply2(llm.MockResponse{Content: "ping received, done"})

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("Run() error: %v", r.err)
		}
		if r.result.Output != "ping received, done" {
			t.Errorf("output = %q", r.result.Output)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}

// ─── Test 9: tracing spans recorded correctly ─────────────────────────────────

func TestReactLoop_TracingSpans(t *testing.T) {
	mock := llm.NewMockProvider("mock").
		QueueToolCall("echo", map[string]any{"msg": "traced"}).
		QueueText("done")

	reg := newTestRegistry(&echoTool{})
	a := newTestAgent(mock, reg)
	tracer := tracing.NewInMemoryTracer(nil)
	runner := newRunner(a, tracer, nil)

	ctx := context.Background()
	_, err := runner.Run(ctx, newTask("trace test"))
	if err != nil {
		t.Fatal(err)
	}

	// Extract trace ID from the agent's context via the spans.
	allTraces := tracer.AllTraces()
	if len(allTraces) == 0 {
		t.Fatal("no traces recorded")
	}
	spans, err := tracer.GetTrace(allTraces[0])
	if err != nil {
		t.Fatal(err)
	}

	ops := make(map[string]int)
	for _, s := range spans {
		ops[s.Operation]++
	}

	if ops["task"] == 0 {
		t.Error("expected at least one 'task' span")
	}
	if ops["llm_call"] < 2 {
		t.Errorf("expected ≥2 'llm_call' spans, got %d", ops["llm_call"])
	}
	if ops["tool_call"] == 0 {
		t.Error("expected at least one 'tool_call' span")
	}
}
