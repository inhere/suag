package agentcore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRuntime_Run_ToolCallThenFinal(t *testing.T) {
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tool := &runtimeTestTool{
		name: "lookup",
		result: &ToolResult{
			Content: "tool-output",
		},
	}
	tools.Register(tool)
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID: "call-1",
				Name:   "lookup",
				Arguments: map[string]any{
					"query": "weather",
				},
			}},
			FinishReason: FinishToolCalls,
			Usage:        Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5},
		}, {
			Content:      "final answer",
			FinishReason: FinishFinal,
			Usage:        Usage{InputTokens: 4, OutputTokens: 3, TotalTokens: 7},
		}},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:       "run-tool-calls",
		AgentID:     "agent-1",
		Instruction: "use a tool",
	})
	if err != nil {
		t.Fatalf("expected tool loop to reach final answer, got %v", err)
	}
	if result.Status != RunCompleted || result.Output != "final answer" {
		t.Fatalf("expected final completed result, got %#v", result)
	}
	if result.Steps != 2 {
		t.Fatalf("expected two model steps, got %#v", result)
	}
	if result.Usage != (Usage{InputTokens: 7, OutputTokens: 5, TotalTokens: 12}) {
		t.Fatalf("expected cumulative usage across model steps, got %#v", result.Usage)
	}
	if len(provider.calls) != 2 {
		t.Fatalf("expected two model calls, got %d", len(provider.calls))
	}
	if len(provider.calls[1].Messages) != 3 {
		t.Fatalf("expected second model call to include user, assistant tool-call, and tool messages, got %#v", provider.calls[1].Messages)
	}
	callMsg := provider.calls[1].Messages[1]
	if callMsg.Role != "assistant" || callMsg.ToolCall == nil || callMsg.ToolCall.Name != "lookup" {
		t.Fatalf("expected assistant tool-call message in second model call, got %#v", callMsg)
	}
	toolMsg := provider.calls[1].Messages[2]
	if toolMsg.Role != "tool" || toolMsg.ToolCall == nil || toolMsg.ToolCall.Name != "lookup" || toolMsg.ToolResult == nil || toolMsg.ToolResult.Content != "tool-output" {
		t.Fatalf("expected tool result message in second model call, got %#v", toolMsg)
	}
	saved := store.runs["run-tool-calls"]
	if saved == nil || saved.Status != RunCompleted {
		t.Fatalf("expected completed state after tool loop, got %#v", saved)
	}
	if len(store.messages["run-tool-calls"]) != 4 {
		t.Fatalf("expected persisted messages to include user, assistant tool-call, tool, assistant, got %#v", store.messages["run-tool-calls"])
	}
	if store.messages["run-tool-calls"][1].Role != "assistant" || store.messages["run-tool-calls"][1].ToolCall == nil {
		t.Fatalf("expected persisted assistant tool-call message, got %#v", store.messages["run-tool-calls"][1])
	}
	if store.messages["run-tool-calls"][2].Role != "tool" || store.messages["run-tool-calls"][2].ToolResult == nil {
		t.Fatalf("expected persisted tool message, got %#v", store.messages["run-tool-calls"][2])
	}
}

func TestRuntime_Run_ToolErrorBecomesMessage(t *testing.T) {
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{
		name: "lookup",
		err:  errors.New("lookup exploded"),
	})
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID: "call-1",
				Name:   "lookup",
			}},
			FinishReason: FinishToolCalls,
		}, {
			Content:      "recovered after tool error",
			FinishReason: FinishFinal,
		}},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:       "run-tool-error",
		AgentID:     "agent-1",
		Instruction: "use failing tool",
	})
	if err != nil {
		t.Fatalf("expected tool error to become a message and continue, got %v", err)
	}
	if result.Status != RunCompleted || result.Output != "recovered after tool error" {
		t.Fatalf("expected final result after tool error recovery, got %#v", result)
	}
	if len(provider.calls) != 2 {
		t.Fatalf("expected second model call after tool failure, got %d calls", len(provider.calls))
	}
	if len(provider.calls[1].Messages) != 3 {
		t.Fatalf("expected second model call to include user, assistant tool-call, and tool messages, got %#v", provider.calls[1].Messages)
	}
	if provider.calls[1].Messages[1].Role != "assistant" || provider.calls[1].Messages[1].ToolCall == nil || provider.calls[1].Messages[1].ToolCall.Name != "lookup" {
		t.Fatalf("expected assistant tool-call message before tool error result, got %#v", provider.calls[1].Messages[1])
	}
	toolMsg := provider.calls[1].Messages[2]
	if toolMsg.Role != "tool" || toolMsg.ToolResult == nil || !strings.Contains(toolMsg.ToolResult.Content, "lookup exploded") {
		t.Fatalf("expected tool failure to be turned into tool message, got %#v", toolMsg)
	}
	saved := store.runs["run-tool-error"]
	if saved == nil || saved.Status != RunCompleted || saved.LastError != "" {
		t.Fatalf("expected tool error not to force terminal failure, got %#v", saved)
	}
}

func TestRuntime_Run_RejectsToolCallMissingRequiredArgumentsBeforeExecution(t *testing.T) {
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tool := &runtimeTestToolWithSchema{
		runtimeTestTool: runtimeTestTool{
			name:        "file_read",
			description: "Read file",
		},
		inputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type": "string",
				},
			},
			"required": []any{"path"},
		},
	}
	tools.Register(tool)
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID:    "call-1",
				Name:      "file_read",
				Arguments: nil,
			}},
			FinishReason: FinishToolCalls,
		}},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:       "run-missing-required-tool-args",
		AgentID:     "agent-1",
		Instruction: "read file without path",
	})
	if result != nil {
		t.Fatalf("expected run to fail before tool execution, got %#v", result)
	}
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected missing required args to surface as ToolError, got %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "missing required argument \"path\"") {
		t.Fatalf("expected required-argument failure, got %v", err)
	}
	if len(tool.calls) != 0 {
		t.Fatalf("expected tool execution to be skipped for invalid call, got %#v", tool.calls)
	}
	if len(store.messages["run-missing-required-tool-args"]) != 1 || store.messages["run-missing-required-tool-args"][0].Role != "user" {
		t.Fatalf("expected invalid tool call not to persist tool transcript, got %#v", store.messages["run-missing-required-tool-args"])
	}
	saved := store.runs["run-missing-required-tool-args"]
	if saved == nil || saved.Status != RunFailed || !strings.Contains(saved.LastError, "missing required argument \"path\"") {
		t.Fatalf("expected failed run state with required argument error, got %#v", saved)
	}
}

func TestRuntime_Run_MaxStepsExceededFailsRun(t *testing.T) {
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{name: "lookup"})
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID: "call-1",
				Name:   "lookup",
			}},
			FinishReason: FinishToolCalls,
		}, {
			Content:      "should-not-complete",
			FinishReason: FinishFinal,
		}},
	}

	_, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		MaxSteps:       5,
	}).Run(context.Background(), RunInput{
		RunID:       "run-max-steps",
		AgentID:     "agent-1",
		Instruction: "limited",
		MaxSteps:    1,
	})
	if err == nil || !strings.Contains(err.Error(), "max steps") {
		t.Fatalf("expected max-steps failure, got %v", err)
	}
	saved := store.runs["run-max-steps"]
	if saved == nil || saved.Status != RunFailed || !strings.Contains(saved.LastError, "max steps") {
		t.Fatalf("expected failed state with max-steps error, got %#v", saved)
	}
}

func TestRuntime_Run_MaxStepsUsesConfigWhenInputUnset(t *testing.T) {
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{name: "lookup"})
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID: "call-1",
				Name:   "lookup",
			}},
			FinishReason: FinishToolCalls,
		}, {
			Content:      "should-not-complete",
			FinishReason: FinishFinal,
		}},
	}

	_, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		MaxSteps:       1,
	}).Run(context.Background(), RunInput{
		RunID:       "run-max-steps-config",
		AgentID:     "agent-1",
		Instruction: "limited by config",
	})
	if err == nil || !strings.Contains(err.Error(), "max steps") {
		t.Fatalf("expected config max-steps failure when input unset, got %v", err)
	}
	saved := store.runs["run-max-steps-config"]
	if saved == nil || saved.Status != RunFailed || !strings.Contains(saved.LastError, "max steps") {
		t.Fatalf("expected failed state with config max-steps error, got %#v", saved)
	}
}

func TestRuntime_Run_MaxToolCallsPerStepExceededFailsRun(t *testing.T) {
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{name: "lookup"})
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{
				{CallID: "call-1", Name: "lookup"},
				{CallID: "call-2", Name: "lookup"},
			},
			FinishReason: FinishToolCalls,
		}},
	}

	_, err := New(Config{
		Provider:            provider,
		Storage:             store,
		Tools:               tools,
		PromptBuilder:       runtimeTestPromptBuilder{},
		ContextBuilder:      runtimeTestContextBuilder{},
		MaxToolCallsPerStep: 1,
	}).Run(context.Background(), RunInput{
		RunID:       "run-max-tool-calls",
		AgentID:     "agent-1",
		Instruction: "too many tools",
	})
	if err == nil || !strings.Contains(err.Error(), "max tool calls") {
		t.Fatalf("expected max-tool-calls failure, got %v", err)
	}
	saved := store.runs["run-max-tool-calls"]
	if saved == nil || saved.Status != RunFailed || !strings.Contains(saved.LastError, "max tool calls") {
		t.Fatalf("expected failed state with max-tool-calls error, got %#v", saved)
	}
}

func TestRuntime_Run_ToolArtifactsPersisted(t *testing.T) {
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{
		name: "lookup",
		result: &ToolResult{
			Content: "tool-output",
			Artifacts: []Artifact{{
				Kind: "file",
				URI:  "artifact://report.txt",
			}},
		},
	})
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls:    []ToolCall{{CallID: "call-1", Name: "lookup"}},
			FinishReason: FinishToolCalls,
		}, {
			Content:      "done",
			FinishReason: FinishFinal,
		}},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:       "run-tool-artifacts",
		AgentID:     "agent-1",
		Instruction: "collect artifact",
	})
	if err != nil {
		t.Fatalf("expected artifact-producing tool loop to succeed, got %v", err)
	}
	if result.Status != RunCompleted {
		t.Fatalf("expected completed result, got %#v", result)
	}
	saved := store.runs["run-tool-artifacts"]
	if saved == nil || len(saved.Artifacts) != 1 || saved.Artifacts[0].URI != "artifact://report.txt" {
		t.Fatalf("expected tool artifacts to be persisted on run state, got %#v", saved)
	}
}

func TestRuntime_Run_FinalArtifactsPersistedOnRunState(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done",
			FinishReason: FinishFinal,
			Artifacts: []Artifact{{
				Kind: "file",
				URI:  "artifact://final-report.txt",
			}},
		}},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          NewToolRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:       "run-final-artifacts",
		AgentID:     "agent-1",
		Instruction: "produce final artifact",
	})
	if err != nil {
		t.Fatalf("expected final artifact run to succeed, got %v", err)
	}
	if result.Status != RunCompleted || result.Output != "done" {
		t.Fatalf("expected completed final result, got %#v", result)
	}
	saved := store.runs["run-final-artifacts"]
	if saved == nil || len(saved.Artifacts) != 1 || saved.Artifacts[0].URI != "artifact://final-report.txt" {
		t.Fatalf("expected final artifacts to be persisted on run state, got %#v", saved)
	}
}

func TestRuntime_Run_FinalMetadataPersistedOnAssistantMessage(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done",
			FinishReason: FinishFinal,
			Metadata: map[string]any{
				"provider": "mock",
				"reason":   "final_authority",
			},
		}},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          NewToolRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:       "run-final-metadata",
		AgentID:     "agent-1",
		Instruction: "produce final metadata",
	})
	if err != nil {
		t.Fatalf("expected final metadata run to succeed, got %v", err)
	}
	if result.Status != RunCompleted || result.Output != "done" {
		t.Fatalf("expected completed final result, got %#v", result)
	}
	persisted := store.messages["run-final-metadata"]
	if len(persisted) != 2 {
		t.Fatalf("expected user and final assistant messages, got %#v", persisted)
	}
	finalMsg := persisted[1]
	if finalMsg.Role != "assistant" || finalMsg.Content != "done" {
		t.Fatalf("expected final assistant authority message, got %#v", finalMsg)
	}
	if finalMsg.Metadata["provider"] != "mock" || finalMsg.Metadata["reason"] != "final_authority" {
		t.Fatalf("expected final message metadata to remain authoritative, got %#v", finalMsg.Metadata)
	}
	saved := store.runs["run-final-metadata"]
	if saved == nil {
		t.Fatal("expected saved run state after final metadata result")
	}
	if len(saved.Metadata) != 0 {
		t.Fatalf("expected final result metadata not to overwrite run metadata, got %#v", saved.Metadata)
	}
}

func TestRuntime_Run_PersistsArtifactsToOptionalArtifactStorage(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done",
			FinishReason: FinishFinal,
			Artifacts: []Artifact{{
				Kind: "file",
				URI:  "artifact://optional-store.txt",
			}},
		}},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          NewToolRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:       "run-artifact-capability-save",
		AgentID:     "agent-1",
		Instruction: "persist artifact through capability",
	})
	if err != nil {
		t.Fatalf("expected run with artifact capability to succeed, got %v", err)
	}
	if result.Status != RunCompleted {
		t.Fatalf("expected completed result, got %#v", result)
	}
	if store.artifactSaveCalls != 2 {
		t.Fatalf("expected artifact capability to be updated on each save, got %d calls", store.artifactSaveCalls)
	}
	if len(store.artifacts["run-artifact-capability-save"]) != 1 || store.artifacts["run-artifact-capability-save"][0].URI != "artifact://optional-store.txt" {
		t.Fatalf("expected artifact capability store to receive final artifacts, got %#v", store.artifacts["run-artifact-capability-save"])
	}
}

func TestRuntime_Run_AfterToolRunsBeforeSuccessfulToolPersist(t *testing.T) {
	store := newRuntimeTestStorage()
	afterToolObserved := false
	store.appendHook = func(_ string, msgs []Message) {
		if len(msgs) == 2 && msgs[0].Role == "assistant" && msgs[1].Role == "tool" && !afterToolObserved {
			t.Fatalf("expected successful tool persist to happen after AfterTool hook")
		}
	}

	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{
		name: "lookup",
		result: &ToolResult{
			Content: "tool-output",
		},
	})
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls:    []ToolCall{{CallID: "call-1", Name: "lookup"}},
			FinishReason: FinishToolCalls,
		}, {
			Content:      "done",
			FinishReason: FinishFinal,
		}},
	}
	hook := &runtimeTestHook{
		afterTool: func(_ *Control) {
			afterToolObserved = true
		},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	}).Run(context.Background(), RunInput{
		RunID:       "run-after-tool-before-persist",
		AgentID:     "agent-1",
		Instruction: "observe ordering",
	})
	if err != nil {
		t.Fatalf("expected successful tool loop, got %v", err)
	}
	if result.Status != RunCompleted {
		t.Fatalf("expected completed result, got %#v", result)
	}
}

func TestRuntime_Run_AfterToolMutationsPersistIntoMessageAndNextContext(t *testing.T) {
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{
		name: "lookup",
		result: &ToolResult{
			Content: "tool-output",
		},
	})
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID: "call-1",
				Name:   "lookup",
				Arguments: map[string]any{
					"query": "original",
				},
			}},
			FinishReason: FinishToolCalls,
		}, {
			Content:      "done",
			FinishReason: FinishFinal,
		}},
	}
	hook := &runtimeTestHook{
		afterToolEvent: func(event *ToolEvent, _ *Control) {
			event.Call = &ToolCall{
				CallID: "call-1-mutated",
				Name:   "lookup-mutated",
				Arguments: map[string]any{
					"query": "mutated",
				},
			}
			event.Result = &ToolResult{
				CallID:  "call-1-mutated",
				Name:    "lookup-mutated",
				Content: "mutated-output",
				Artifacts: []Artifact{{
					Kind: "file",
					URI:  "artifact://mutated.txt",
				}},
				Metadata: map[string]any{
					"source": "after_tool",
				},
			}
		},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	}).Run(context.Background(), RunInput{
		RunID:       "run-after-tool-mutation",
		AgentID:     "agent-1",
		Instruction: "mutate tool result",
	})
	if err != nil {
		t.Fatalf("expected after-tool mutation path to succeed, got %v", err)
	}
	if result.Status != RunCompleted {
		t.Fatalf("expected completed result, got %#v", result)
	}
	if len(store.messages["run-after-tool-mutation"]) != 4 {
		t.Fatalf("expected user, assistant tool-call, tool, assistant messages, got %#v", store.messages["run-after-tool-mutation"])
	}
	callMsg := store.messages["run-after-tool-mutation"][1]
	if callMsg.Role != "assistant" || callMsg.ToolCall == nil || callMsg.ToolCall.Name != "lookup-mutated" || callMsg.ToolCall.CallID != "call-1-mutated" {
		t.Fatalf("expected persisted assistant tool call to reflect after-tool mutation, got %#v", callMsg)
	}
	toolMsg := store.messages["run-after-tool-mutation"][2]
	if toolMsg.ToolResult == nil || toolMsg.ToolResult.Content != "mutated-output" {
		t.Fatalf("expected persisted tool result content to reflect after-tool mutation, got %#v", toolMsg.ToolResult)
	}
	if toolMsg.ToolResult == nil || toolMsg.ToolResult.Metadata["source"] != "after_tool" {
		t.Fatalf("expected persisted tool result metadata to reflect after-tool mutation, got %#v", toolMsg.ToolResult)
	}
	if len(toolMsg.ToolResult.Artifacts) != 1 || toolMsg.ToolResult.Artifacts[0].URI != "artifact://mutated.txt" {
		t.Fatalf("expected persisted tool result artifacts to reflect after-tool mutation, got %#v", toolMsg.ToolResult)
	}
	if len(provider.calls) != 2 {
		t.Fatalf("expected second model call after tool loop, got %d", len(provider.calls))
	}
	if len(provider.calls[1].Messages) != 3 {
		t.Fatalf("expected next model context to include user, assistant tool-call, and tool result, got %#v", provider.calls[1].Messages)
	}
	nextCallMsg := provider.calls[1].Messages[1]
	if nextCallMsg.Role != "assistant" || nextCallMsg.ToolCall == nil || nextCallMsg.ToolCall.Name != "lookup-mutated" {
		t.Fatalf("expected next model context to include mutated assistant tool call, got %#v", nextCallMsg)
	}
	nextMsg := provider.calls[1].Messages[2]
	if nextMsg.ToolResult == nil || nextMsg.ToolResult.Content != "mutated-output" {
		t.Fatalf("expected next model context to include mutated tool result, got %#v", nextMsg)
	}
	saved := store.runs["run-after-tool-mutation"]
	if saved == nil || len(saved.Artifacts) != 1 || saved.Artifacts[0].URI != "artifact://mutated.txt" {
		t.Fatalf("expected mutated artifacts to persist onto run state, got %#v", saved)
	}
}

func TestRuntime_Run_BeforeToolMutationsReachToolExecution(t *testing.T) {
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tool := &runtimeTestTool{
		name: "lookup",
		result: &ToolResult{
			Content: "tool-output",
		},
	}
	tools.Register(tool)
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID: "call-1",
				Name:   "lookup",
				Arguments: map[string]any{
					"query": "original",
				},
			}},
			FinishReason: FinishToolCalls,
		}, {
			Content:      "done",
			FinishReason: FinishFinal,
		}},
	}
	hook := &runtimeTestHook{
		beforeToolEvent: func(event *ToolEvent, _ *Control) {
			event.Call = &ToolCall{
				CallID: "call-1-mutated",
				Name:   "lookup",
				Arguments: map[string]any{
					"query": "mutated",
				},
			}
		},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	}).Run(context.Background(), RunInput{
		RunID:       "run-before-tool-mutation",
		AgentID:     "agent-1",
		Instruction: "mutate call before tool",
	})
	if err != nil {
		t.Fatalf("expected before-tool mutation path to succeed, got %v", err)
	}
	if result.Status != RunCompleted {
		t.Fatalf("expected completed result, got %#v", result)
	}
	if len(tool.calls) != 1 {
		t.Fatalf("expected one tool execution, got %#v", tool.calls)
	}
	if tool.calls[0].CallID != "call-1-mutated" {
		t.Fatalf("expected mutated call id to reach tool execution, got %#v", tool.calls[0])
	}
	if got := tool.calls[0].Arguments["query"]; got != "mutated" {
		t.Fatalf("expected mutated arguments to reach tool execution, got %#v", tool.calls[0])
	}
}

func TestRuntime_Run_BeforeToolControlHonoredAndAfterToolControlRejected(t *testing.T) {
	t.Run("before tool yield", func(t *testing.T) {
		store := newRuntimeTestStorage()
		tools := NewToolRegistry()
		tool := &runtimeTestTool{name: "lookup"}
		tools.Register(tool)
		provider := &runtimeTestProvider{
			results: []*GenerateResult{{
				ToolCalls:    []ToolCall{{CallID: "call-1", Name: "lookup"}},
				FinishReason: FinishToolCalls,
			}},
		}
		hook := &runtimeTestHook{
			beforeTool: func(ctrl *Control) {
				ctrl.Yield("pause_before_tool", map[string]any{"stage": "before_tool"})
			},
		}

		result, err := New(Config{
			Provider:       provider,
			Storage:        store,
			Tools:          tools,
			PromptBuilder:  runtimeTestPromptBuilder{},
			ContextBuilder: runtimeTestContextBuilder{},
			Hooks:          []Hook{hook},
		}).Run(context.Background(), RunInput{
			RunID:       "run-before-tool-yield",
			AgentID:     "agent-1",
			Instruction: "pause before tool",
		})
		if err != nil {
			t.Fatalf("expected before-tool yield to return result, got %v", err)
		}
		if result.Status != RunYielded || result.Yield == nil || result.Yield.Reason != "pause_before_tool" {
			t.Fatalf("expected yielded result from before-tool control, got %#v", result)
		}
		if len(tool.calls) != 0 {
			t.Fatalf("expected tool execution to be skipped after before-tool yield, got %#v", tool.calls)
		}
	})

	t.Run("after tool short circuit rejected", func(t *testing.T) {
		store := newRuntimeTestStorage()
		tools := NewToolRegistry()
		tool := &runtimeTestTool{
			name: "lookup",
			result: &ToolResult{
				Content: "tool-output",
			},
		}
		tools.Register(tool)
		provider := &runtimeTestProvider{
			results: []*GenerateResult{{
				ToolCalls:    []ToolCall{{CallID: "call-1", Name: "lookup"}},
				FinishReason: FinishToolCalls,
			}, {
				Content:      "final after tool",
				FinishReason: FinishFinal,
			}},
		}
		hook := &runtimeTestHook{
			afterTool: func(ctrl *Control) {
				ctrl.ShortCircuit(&RunResult{
					Status: RunCompleted,
					Output: "after-tool-short-circuit",
				})
			},
		}

		result, err := New(Config{
			Provider:       provider,
			Storage:        store,
			Tools:          tools,
			PromptBuilder:  runtimeTestPromptBuilder{},
			ContextBuilder: runtimeTestContextBuilder{},
			Hooks:          []Hook{hook},
		}).Run(context.Background(), RunInput{
			RunID:       "run-after-tool-short-circuit",
			AgentID:     "agent-1",
			Instruction: "finish after tool",
		})
		_ = result
		var hookErr *HookError
		if !errors.As(err, &hookErr) {
			t.Fatalf("expected after-tool control to be rejected with hook error, got %T %v", err, err)
		}
		if !strings.Contains(err.Error(), "after_tool control is not supported") {
			t.Fatalf("expected explicit after-tool control rejection, got %v", err)
		}
		if len(store.messages["run-after-tool-short-circuit"]) != 3 {
			t.Fatalf("expected assistant tool-call and tool execution to be persisted before after-tool rejection, got %#v", store.messages["run-after-tool-short-circuit"])
		}
		if store.messages["run-after-tool-short-circuit"][1].Role != "assistant" || store.messages["run-after-tool-short-circuit"][1].ToolCall == nil {
			t.Fatalf("expected persisted assistant tool-call authority message before after-tool rejection, got %#v", store.messages["run-after-tool-short-circuit"][1])
		}
		if store.messages["run-after-tool-short-circuit"][2].Role != "tool" {
			t.Fatalf("expected persisted tool authority message before after-tool rejection, got %#v", store.messages["run-after-tool-short-circuit"][2])
		}
	})
}

func TestRuntime_Run_AfterToolErrorStillPersistsToolMessage(t *testing.T) {
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{
		name: "lookup",
		result: &ToolResult{
			Content: "tool-output",
		},
	})
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls:    []ToolCall{{CallID: "call-1", Name: "lookup"}},
			FinishReason: FinishToolCalls,
		}},
	}
	hook := &runtimeTestHook{
		afterToolErr: errors.New("after tool boom"),
	}

	_, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	}).Run(context.Background(), RunInput{
		RunID:       "run-after-tool-error",
		AgentID:     "agent-1",
		Instruction: "execute tool then fail hook",
	})

	var hookErr *HookError
	if !errors.As(err, &hookErr) {
		t.Fatalf("expected after-tool error to surface as hook error, got %T %v", err, err)
	}
	if len(store.messages["run-after-tool-error"]) != 3 {
		t.Fatalf("expected user, assistant tool-call, and tool messages to be persisted before after-tool failure, got %#v", store.messages["run-after-tool-error"])
	}
	if store.messages["run-after-tool-error"][1].Role != "assistant" || store.messages["run-after-tool-error"][1].ToolCall == nil {
		t.Fatalf("expected persisted assistant tool-call message before after-tool failure, got %#v", store.messages["run-after-tool-error"][1])
	}
	if store.messages["run-after-tool-error"][2].Role != "tool" || store.messages["run-after-tool-error"][2].ToolResult == nil || store.messages["run-after-tool-error"][2].ToolResult.Content != "tool-output" {
		t.Fatalf("expected persisted tool message before after-tool failure, got %#v", store.messages["run-after-tool-error"][2])
	}
	saved := store.runs["run-after-tool-error"]
	if saved == nil || saved.Status != RunFailed {
		t.Fatalf("expected failed state after after-tool error, got %#v", saved)
	}
}

func TestRuntime_Run_AfterToolControlDoesNotStopLaterAfterToolHooks(t *testing.T) {
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{
		name: "lookup",
		result: &ToolResult{
			Content: "tool-output",
		},
	})
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls:    []ToolCall{{CallID: "call-1", Name: "lookup"}},
			FinishReason: FinishToolCalls,
		}},
	}

	secondHookRan := false
	firstHook := &runtimeTestHook{
		afterTool: func(ctrl *Control) {
			ctrl.ShortCircuit(&RunResult{
				Status: RunCompleted,
				Output: "not-allowed",
			})
		},
	}
	secondHook := &runtimeTestHook{
		afterTool: func(_ *Control) {
			secondHookRan = true
		},
	}

	_, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{firstHook, secondHook},
	}).Run(context.Background(), RunInput{
		RunID:       "run-after-tool-multi-hooks",
		AgentID:     "agent-1",
		Instruction: "execute and reject control",
	})

	if !secondHookRan {
		t.Fatal("expected later after-tool hooks to still run after an earlier hook sets control")
	}
	var hookErr *HookError
	if !errors.As(err, &hookErr) {
		t.Fatalf("expected after-tool control rejection to surface as hook error, got %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "after_tool control is not supported") {
		t.Fatalf("expected explicit after-tool control rejection, got %v", err)
	}
	if len(store.messages["run-after-tool-multi-hooks"]) != 3 ||
		store.messages["run-after-tool-multi-hooks"][1].Role != "assistant" ||
		store.messages["run-after-tool-multi-hooks"][2].Role != "tool" {
		t.Fatalf("expected assistant tool-call and tool messages to be persisted before after-tool control rejection, got %#v", store.messages["run-after-tool-multi-hooks"])
	}
}

func TestRuntime_Run_ToolCallKeepsAssistantTextAndErrorMark(t *testing.T) {
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{name: "ok", result: &ToolResult{Content: "fine"}})
	tools.Register(&runtimeTestTool{name: "bad", err: errors.New("boom")})
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content: "let me check",
			ToolCalls: []ToolCall{
				{CallID: "c1", Name: "ok", Arguments: map[string]any{}},
				{CallID: "c2", Name: "bad", Arguments: map[string]any{}},
			},
			FinishReason: FinishToolCalls,
		}, {Content: "done", FinishReason: FinishFinal}},
	}
	_, err := New(Config{
		Provider: provider, Storage: store, Tools: tools,
		PromptBuilder: runtimeTestPromptBuilder{}, ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{RunID: "run-lead", AgentID: "a", Instruction: "go"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	msgs := store.messages["run-lead"]
	// user, assistant(c1), tool(c1), assistant(c2), tool(c2), final
	if len(msgs) != 6 {
		t.Fatalf("unexpected messages: %#v", msgs)
	}
	if msgs[1].Content != "let me check" || msgs[1].ToolCall == nil {
		t.Fatalf("first tool-call message must keep model text, got %#v", msgs[1])
	}
	if msgs[3].Content != "" {
		t.Fatalf("parallel tool-call message must not repeat text, got %#v", msgs[3])
	}
	if msgs[2].ToolResult.IsError {
		t.Fatalf("successful result must not be marked: %#v", msgs[2].ToolResult)
	}
	if !msgs[4].ToolResult.IsError {
		t.Fatalf("failed result must carry IsError, got %#v", msgs[4].ToolResult)
	}
}

func TestRuntime_Run_MaxStepsWrapUp(t *testing.T) {
	newRun := func(results ...*GenerateResult) (*runtimeTestStorage, *runtimeTestProvider, *RunResult, error) {
		store := newRuntimeTestStorage()
		tools := NewToolRegistry()
		tools.Register(&runtimeTestTool{name: "lookup"})
		provider := &runtimeTestProvider{results: results}
		res, err := New(Config{
			Provider:       provider,
			Storage:        store,
			Tools:          tools,
			PromptBuilder:  runtimeTestPromptBuilder{},
			ContextBuilder: runtimeTestContextBuilder{},
			MaxSteps:       1,
			MaxStepsWrapUp: "WRAP UP",
		}).Run(context.Background(), RunInput{RunID: "run-wrap", AgentID: "agent-1", Instruction: "explore"})
		return store, provider, res, err
	}
	call := &GenerateResult{ToolCalls: []ToolCall{{CallID: "call-1", Name: "lookup"}}, FinishReason: FinishToolCalls}

	t.Run("final answer completes the run", func(t *testing.T) {
		store, provider, res, err := newRun(call, &GenerateResult{Content: "summary so far", FinishReason: FinishFinal})
		if err != nil || res == nil || res.Status != RunCompleted || res.Output != "summary so far" {
			t.Fatalf("expected completed wrap-up run, got %#v, %v", res, err)
		}
		last := provider.calls[1].Messages[len(provider.calls[1].Messages)-1]
		if last.Role != "user" || last.Content != "WRAP UP" {
			t.Fatalf("expected wrap-up instruction last in the request, got %#v", last)
		}
		for _, m := range store.messages["run-wrap"] {
			if m.Content == "WRAP UP" {
				t.Fatalf("wrap-up instruction must not be persisted")
			}
		}
	})
	t.Run("more tool calls still fail", func(t *testing.T) {
		store, provider, _, err := newRun(call, call)
		if err == nil || !strings.Contains(err.Error(), "max steps") {
			t.Fatalf("expected max-steps failure, got %v", err)
		}
		if len(provider.calls) != 2 || store.runs["run-wrap"].Status != RunFailed {
			t.Fatalf("expected exactly one wrap-up call and a failed run")
		}
	})
}
