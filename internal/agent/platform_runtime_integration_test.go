package agent_test

import (
	"context"
	"testing"

	"github.com/inhere/axon/internal/platform/llm"
	platformmemory "github.com/inhere/axon/internal/platform/memory"
	platformtools "github.com/inhere/axon/internal/platform/tools"
	"github.com/inhere/axon/pkg/agentcore"
	contextpkg "github.com/inhere/axon/pkg/agentcore/context"
	"github.com/inhere/axon/pkg/agentcore/files"
)

type yieldBeforeSecondModelRuntimeHook struct {
	agentcore.NopHook
	beforeModelCalls int
}

func (h *yieldBeforeSecondModelRuntimeHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	h.beforeModelCalls++
	if h.beforeModelCalls == 2 {
		ctrl.Yield("await_resume", map[string]any{"step": "after_tool"})
	}
	return nil
}

func TestAgentcoreRuntime_WithPlatformComponents_PersistsAndReplaysCompleteToolTranscript(t *testing.T) {
	runID := "resume-with-platform-components"
	store := files.NewStorage(t.TempDir())
	memStore := platformmemory.NewInMemStore(platformmemory.InMemConfig{})
	mem := platformmemory.NewRuntimeMemory(memStore)

	reg := platformtools.NewRegistry()
	reg.Register(&coreEchoTool{})

	yieldHook := &yieldBeforeSecondModelRuntimeHook{}
	firstRuntime := agentcore.New(agentcore.Config{
		Provider: llm.NewMockProvider("platform-mock").QueueToolCall("echo", map[string]any{"msg": "persisted"}),
		Storage:  store,
		Memory:   mem,
		Tools:    reg,
		Hooks:    []agentcore.Hook{yieldHook},
		ContextBuilder: contextpkg.TranscriptWindow(contextpkg.WindowConfig{
			MaxMessages: 0,
		}),
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
		t.Fatalf("expected persisted assistant tool-call message from platform components, got %#v", messages[1])
	}
	if messages[2].Role != "tool" || messages[2].ToolResult == nil || messages[2].ToolResult.Content != "echo: persisted" {
		t.Fatalf("expected persisted tool result content from platform components, got %#v", messages[2])
	}

	resumeProvider := llm.NewMockProvider("platform-mock").QueueText("final after resume")
	resumeRuntime := agentcore.New(agentcore.Config{
		Provider: resumeProvider,
		Storage:  store,
		Memory:   mem,
		Tools:    reg,
		ContextBuilder: contextpkg.TranscriptWindow(contextpkg.WindowConfig{
			MaxMessages: 0,
		}),
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
		t.Fatalf("expected resumed provider to receive assistant tool-call message from platform components, got %#v", providerCalls[0].Messages[1])
	}
	if providerCalls[0].Messages[2].ToolResult == nil || providerCalls[0].Messages[2].ToolResult.ToolCallID != "call-echo" || providerCalls[0].Messages[2].ToolResult.Content != "echo: persisted" {
		t.Fatalf("expected resumed provider to receive persisted tool result from platform components, got %#v", providerCalls[0].Messages[2])
	}
}
