package orchestrator_test

import (
	"context"
	"testing"

	"github.com/inhere/axon/internal/llm"
	"github.com/inhere/axon/internal/orchestrator"
	"github.com/inhere/axon/internal/tools"
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
