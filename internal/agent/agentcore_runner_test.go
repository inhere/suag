package agent_test

import (
	"context"
	"testing"

	"github.com/inhere/axon/internal/agent"
	"github.com/inhere/axon/internal/agentcorestorage"
	"github.com/inhere/axon/internal/llm"
	"github.com/inhere/axon/internal/tools"
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
