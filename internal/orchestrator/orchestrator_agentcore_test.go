package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"github.com/inhere/axon/internal/hitl"
	"github.com/inhere/axon/internal/llm"
	"github.com/inhere/axon/internal/orchestrator"
	"github.com/inhere/axon/internal/tools"
	"github.com/inhere/axon/internal/tracing"
)

type echoTool struct{}

func (e *echoTool) Name() string           { return "echo" }
func (e *echoTool) Description() string    { return "echo the msg argument" }
func (e *echoTool) Kind() tools.Kind       { return tools.KindNative }
func (e *echoTool) Schema() llm.ToolSchema { return llm.ToolSchema{Name: "echo"} }
func (e *echoTool) Execute(_ context.Context, args map[string]any) (string, error) {
	msg, _ := args["msg"].(string)
	return "echo: " + msg, nil
}

type failTool struct{ callCount int }

func (f *failTool) Name() string           { return "fail_tool" }
func (f *failTool) Description() string    { return "always fails" }
func (f *failTool) Kind() tools.Kind       { return tools.KindNative }
func (f *failTool) Schema() llm.ToolSchema { return llm.ToolSchema{Name: "fail_tool"} }
func (f *failTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	f.callCount++
	return "", context.DeadlineExceeded
}

func TestOrchestrator_RunTask_WithAgentcoreRuntime(t *testing.T) {
	mock := llm.NewMockProvider("mock").QueueText("hello from orchestrator agentcore")

	orch := orchestrator.New(orchestrator.Config{
		LLM:              mock,
		Registry:         tools.NewRegistry(),
		UseAgentcore:     true,
		AgentcoreRunRoot: t.TempDir(),
	})

	result, err := orch.RunTask(context.Background(), "say hello")
	if err != nil {
		t.Fatalf("RunTask() error: %v", err)
	}
	if result.Output != "hello from orchestrator agentcore" {
		t.Fatalf("output = %q, want final answer", result.Output)
	}
	if mock.CallCount() != 1 {
		t.Fatalf("LLM called %d times, want 1", mock.CallCount())
	}
}

func TestOrchestrator_RunTask_WithAgentcoreRuntime_BridgesTools(t *testing.T) {
	mock := llm.NewMockProvider("mock").
		QueueToolCall("echo", map[string]any{"msg": "hello"}).
		QueueText("tool flow complete")

	reg := tools.NewRegistry()
	reg.MustRegister(&echoTool{})

	orch := orchestrator.New(orchestrator.Config{
		LLM:              mock,
		Registry:         reg,
		UseAgentcore:     true,
		AgentcoreRunRoot: t.TempDir(),
	})

	result, err := orch.RunTask(context.Background(), "use echo")
	if err != nil {
		t.Fatalf("RunTask() error: %v", err)
	}
	if result.Output != "tool flow complete" {
		t.Fatalf("output = %q, want final answer", result.Output)
	}
	if mock.CallCount() != 2 {
		t.Fatalf("LLM called %d times, want 2", mock.CallCount())
	}
}

func TestOrchestrator_RunTask_WithAgentcoreRuntime_EmitsTracingSpans(t *testing.T) {
	mock := llm.NewMockProvider("mock").
		QueueToolCall("echo", map[string]any{"msg": "hello"}).
		QueueText("tool flow complete")

	reg := tools.NewRegistry()
	reg.MustRegister(&echoTool{})
	tracer := tracing.NewInMemoryTracer(nil)

	orch := orchestrator.New(orchestrator.Config{
		LLM:              mock,
		Registry:         reg,
		Tracer:           tracer,
		UseAgentcore:     true,
		AgentcoreRunRoot: t.TempDir(),
	})

	result, err := orch.RunTask(context.Background(), "use echo")
	if err != nil {
		t.Fatalf("RunTask() error: %v", err)
	}
	if result.Output != "tool flow complete" {
		t.Fatalf("output = %q, want final answer", result.Output)
	}

	traceIDs := tracer.AllTraces()
	if len(traceIDs) != 1 {
		t.Fatalf("trace count = %d, want 1", len(traceIDs))
	}
	spans, err := tracer.GetTrace(traceIDs[0])
	if err != nil {
		t.Fatalf("GetTrace() error: %v", err)
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

func TestOrchestrator_RunTask_WithAgentcoreRuntime_PassesThroughHITL(t *testing.T) {
	ft := &failTool{}
	mock := llm.NewMockProvider("mock").
		QueueToolCall("fail_tool", nil).
		QueueToolCall("fail_tool", nil).
		QueueToolCall("fail_tool", nil).
		QueueText("Recovered after human intervention.")

	reg := tools.NewRegistry()
	reg.MustRegister(ft)
	hitlMgr := hitl.NewInMemoryManager(nil)

	orch := orchestrator.New(orchestrator.Config{
		LLM:              mock,
		Registry:         reg,
		HITL:             hitlMgr,
		UseAgentcore:     true,
		AgentcoreRunRoot: t.TempDir(),
	})

	type runResult struct {
		output string
		err    error
	}
	ch := make(chan runResult, 1)
	go func() {
		result, err := orch.RunTask(context.Background(), "use fail_tool")
		if err != nil {
			ch <- runResult{err: err}
			return
		}
		ch <- runResult{output: result.Output}
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
			t.Fatalf("RunTask() error after HITL: %v", r.err)
		}
		if r.output != "Recovered after human intervention." {
			t.Fatalf("output = %q", r.output)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for orchestrator task to complete after HITL")
	}
}

func TestOrchestrator_RunTask_WithAgentcoreRuntime_UsesConfiguredFailThreshold(t *testing.T) {
	ft := &failTool{}
	mock := llm.NewMockProvider("mock").
		QueueToolCall("fail_tool", nil).
		QueueToolCall("fail_tool", nil).
		QueueText("Recovered after human intervention.")

	reg := tools.NewRegistry()
	reg.MustRegister(ft)
	hitlMgr := hitl.NewInMemoryManager(nil)

	orch := orchestrator.New(orchestrator.Config{
		LLM:              mock,
		Registry:         reg,
		HITL:             hitlMgr,
		UseAgentcore:     true,
		AgentcoreRunRoot: t.TempDir(),
		FailThreshold:    2,
	})

	type runResult struct {
		output string
		err    error
	}
	ch := make(chan runResult, 1)
	go func() {
		result, err := orch.RunTask(context.Background(), "use fail_tool")
		if err != nil {
			ch <- runResult{err: err}
			return
		}
		ch <- runResult{output: result.Output}
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
	if ft.callCount != 2 {
		t.Fatalf("fail_tool called %d times, want 2 before HITL", ft.callCount)
	}

	if err := hitlMgr.Respond(ticket.ID, "Stop using fail_tool, just finish."); err != nil {
		t.Fatalf("Respond error: %v", err)
	}

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("RunTask() error after HITL: %v", r.err)
		}
		if r.output != "Recovered after human intervention." {
			t.Fatalf("output = %q", r.output)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for orchestrator task to complete after HITL")
	}
}

func TestOrchestrator_RunTask_DefaultsToAgentcoreRuntime(t *testing.T) {
	mock := llm.NewMockProvider("mock").QueueText("hello from default runtime")
	tracer := tracing.NewInMemoryTracer(nil)

	orch := orchestrator.New(orchestrator.Config{
		LLM:              mock,
		Registry:         tools.NewRegistry(),
		Tracer:           tracer,
		AgentcoreRunRoot: t.TempDir(),
	})

	result, err := orch.RunTask(context.Background(), "say hello")
	if err != nil {
		t.Fatalf("RunTask() error: %v", err)
	}
	if result.Output != "hello from default runtime" {
		t.Fatalf("output = %q, want final answer", result.Output)
	}

	traceIDs := tracer.AllTraces()
	if len(traceIDs) != 1 {
		t.Fatalf("trace count = %d, want 1", len(traceIDs))
	}
	spans, err := tracer.GetTrace(traceIDs[0])
	if err != nil {
		t.Fatalf("GetTrace() error: %v", err)
	}

	foundTaskSpan := false
	for _, span := range spans {
		if span.Operation == "task" {
			foundTaskSpan = true
			break
		}
	}
	if !foundTaskSpan {
		t.Fatal("expected default path to emit agentcore task span")
	}
}

func TestOrchestrator_RunTask_CanFallbackToReactRuntime(t *testing.T) {
	mock := llm.NewMockProvider("mock").QueueText("hello from react runtime")

	orch := orchestrator.New(orchestrator.Config{
		LLM:                mock,
		Registry:           tools.NewRegistry(),
		SingleAgentRuntime: "react",
		AgentcoreRunRoot:   t.TempDir(),
	})

	result, err := orch.RunTask(context.Background(), "say hello")
	if err != nil {
		t.Fatalf("RunTask() error: %v", err)
	}
	if result.Output != "hello from react runtime" {
		t.Fatalf("output = %q, want final answer", result.Output)
	}
	if mock.CallCount() != 1 {
		t.Fatalf("LLM called %d times, want 1", mock.CallCount())
	}
}
