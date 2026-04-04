package agentcoreadapters_test

import (
	"context"
	"testing"

	"github.com/inhere/axon/internal/llm"
	internaltools "github.com/inhere/axon/internal/tools"
	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/internal/agentcoreadapters/internalllm"
	adapterinternaltools "github.com/inhere/axon/internal/agentcoreadapters/internaltools"
	"github.com/inhere/axon/pkg/agentcore/files"
)

type echoTool struct{}

func (echoTool) Name() string        { return "echo" }
func (echoTool) Description() string { return "Echoes a message" }
func (echoTool) Kind() internaltools.Kind {
	return internaltools.KindNative
}
func (echoTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        "echo",
		Description: "Echoes a message",
		Parameters: llm.SchemaProperty{
			Type: "object",
			Properties: map[string]*llm.SchemaProperty{
				"text": {Type: "string"},
			},
			Required: []string{"text"},
		},
	}
}
func (echoTool) Execute(_ context.Context, args map[string]any) (string, error) {
	return "tool:" + args["text"].(string), nil
}

type yieldBeforeSecondModelHook struct {
	agentcore.NopHook
	beforeModelCalls int
}

func (h *yieldBeforeSecondModelHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	h.beforeModelCalls++
	if h.beforeModelCalls == 2 {
		ctrl.Yield("await_resume", map[string]any{"step": "after_tool"})
	}
	return nil
}

type staticPromptBuilder struct {
	prompt string
}

func (b staticPromptBuilder) BuildSystemPrompt(context.Context, *agentcore.RunState) (string, error) {
	return b.prompt, nil
}

type stateMessagesBuilder struct{}

func (stateMessagesBuilder) BuildMessages(_ context.Context, state *agentcore.RunState) ([]agentcore.Message, error) {
	if state == nil || len(state.Messages) == 0 {
		return nil, nil
	}
	msgs := make([]agentcore.Message, len(state.Messages))
	copy(msgs, state.Messages)
	return msgs, nil
}

func TestRuntime_WithRealAdapterPackages_PersistsAndReplaysCompleteToolTranscript(t *testing.T) {
	runID := "resume-with-real-adapters"
	store := files.NewStorage(t.TempDir())

	reg := internaltools.NewRegistry()
	reg.MustRegister(echoTool{})

	yieldHook := &yieldBeforeSecondModelHook{}

	firstRuntime := agentcore.New(agentcore.Config{
		Provider: internalllm.NewProvider(
			llm.NewMockProvider("internal-mock").QueueToolCall("echo", map[string]any{"text": "persisted"}),
		),
		Storage:        store,
		Tools:          adapterinternaltools.NewRegistry(reg),
		PromptBuilder:  staticPromptBuilder{prompt: "system"},
		ContextBuilder: stateMessagesBuilder{},
		Hooks:          []agentcore.Hook{yieldHook},
	})

	result, err := firstRuntime.Run(context.Background(), agentcore.RunInput{
		RunID:       runID,
		AgentID:     "agent-1",
		Instruction: "use a tool and pause",
	})
	if err != nil {
		t.Fatalf("expected first run to yield cleanly, got %v", err)
	}
	if result.Status != agentcore.RunYielded || result.Yield == nil || result.Yield.Reason != "await_resume" {
		t.Fatalf("expected yielded result before second model call, got %#v", result)
	}

	messages, err := store.LoadMessages(context.Background(), runID)
	if err != nil {
		t.Fatalf("expected persisted messages, got %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("expected user, assistant tool-call, and tool messages after pause, got %#v", messages)
	}
	if messages[1].Role != "assistant" || messages[1].ToolCall == nil || messages[1].ToolCall.CallID != "call-echo" {
		t.Fatalf("expected persisted assistant tool-call message from real adapters, got %#v", messages[1])
	}
	if messages[2].Role != "tool" || messages[2].ToolResult == nil || messages[2].ToolResult.Content != "tool:persisted" {
		t.Fatalf("expected persisted tool result content from real adapters, got %#v", messages[2])
	}

	resumeProvider := llm.NewMockProvider("internal-mock").QueueText("final after resume")
	resumeRuntime := agentcore.New(agentcore.Config{
		Provider:       internalllm.NewProvider(resumeProvider),
		Storage:        store,
		Tools:          adapterinternaltools.NewRegistry(reg),
		PromptBuilder:  staticPromptBuilder{prompt: "system"},
		ContextBuilder: stateMessagesBuilder{},
	})

	result, err = resumeRuntime.Run(context.Background(), agentcore.RunInput{
		RunID:      runID,
		ResumeMode: agentcore.ResumeFromState,
	})
	if err != nil {
		t.Fatalf("expected resume to complete, got %v", err)
	}
	if result.Status != agentcore.RunCompleted || result.Output != "final after resume" {
		t.Fatalf("expected completed resumed result, got %#v", result)
	}

	providerCalls := resumeProvider.Calls()
	if len(providerCalls) != 1 {
		t.Fatalf("expected one resumed provider call, got %d", len(providerCalls))
	}
	if len(providerCalls[0].Messages) != 3 {
		t.Fatalf("expected resumed provider to receive prior user, assistant tool-call, and tool messages, got %#v", providerCalls[0].Messages)
	}
	if providerCalls[0].Messages[1].Role != "assistant" || providerCalls[0].Messages[1].ToolCall == nil || providerCalls[0].Messages[1].ToolCall.ID != "call-echo" {
		t.Fatalf("expected resumed provider to receive assistant tool-call message from real adapters, got %#v", providerCalls[0].Messages[1])
	}
	if providerCalls[0].Messages[2].ToolResult == nil || providerCalls[0].Messages[2].ToolResult.ToolCallID != "call-echo" || providerCalls[0].Messages[2].ToolResult.Content != "tool:persisted" {
		t.Fatalf("expected resumed provider to receive persisted tool result from real adapters, got %#v", providerCalls[0].Messages[2])
	}
}
