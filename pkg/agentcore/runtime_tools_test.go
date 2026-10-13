package agentcore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/inhere/suga/pkg/agentcore/tools"
)

func TestRuntime_Run_ToolCallThenFinal(t *testing.T) {
	store := newRuntimeTestStorage()
	reg := tools.NewRegistry()
	tool := &runtimeTestTool{
		name: "lookup",
		result: &ToolResult{
			Content: "tool-output",
		},
	}
	reg.MustRegister(tool)
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
		Tools:          reg,
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
	reg := tools.NewRegistry()
	reg.MustRegister(&runtimeTestTool{
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
		Tools:          reg,
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

// A call missing a required argument is not run; the model gets an error
// result naming the argument and the run goes on (it used to fail the run).
func TestRuntime_Run_AnswersToolCallMissingRequiredArguments(t *testing.T) {
	store := newRuntimeTestStorage()
	reg := tools.NewRegistry()
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
	reg.MustRegister(tool)
	provider := &runtimeTestProvider{
		results: []*GenerateResult{
			{ToolCalls: []ToolCall{{CallID: "call-1", Name: "file_read", Arguments: nil}}, FinishReason: FinishToolCalls},
			{Content: "asked again", FinishReason: FinishFinal},
		},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          reg,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:       "run-missing-required-tool-args",
		AgentID:     "agent-1",
		Instruction: "read file without path",
	})
	if err != nil || result == nil || result.Output != "asked again" {
		t.Fatalf("expected the run to go on, got %#v, %v", result, err)
	}
	if len(tool.calls) != 0 {
		t.Fatalf("expected tool execution to be skipped for invalid call, got %#v", tool.calls)
	}
	msgs := store.messages["run-missing-required-tool-args"]
	var toolMsg *Message
	for i := range msgs {
		if msgs[i].Role == "tool" {
			toolMsg = &msgs[i]
		}
	}
	if toolMsg == nil || !toolMsg.ToolResult.IsError || !strings.Contains(toolMsg.Content, "missing required argument \"path\"") {
		t.Fatalf("expected an error result naming the argument, got %#v", toolMsg)
	}
}

func TestRuntime_Run_MaxStepsExceededFailsRun(t *testing.T) {
	store := newRuntimeTestStorage()
	reg := tools.NewRegistry()
	reg.MustRegister(&runtimeTestTool{name: "lookup"})
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
		Tools:          reg,
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
	reg := tools.NewRegistry()
	reg.MustRegister(&runtimeTestTool{name: "lookup"})
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
		Tools:          reg,
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
	reg := tools.NewRegistry()
	reg.MustRegister(&runtimeTestTool{name: "lookup"})
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
		Tools:               reg,
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
	reg := tools.NewRegistry()
	reg.MustRegister(&runtimeTestTool{
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
		Tools:          reg,
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
		Tools:          tools.NewRegistry(),
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
		Tools:          tools.NewRegistry(),
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
		Tools:          tools.NewRegistry(),
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

func TestRuntime_Run_PrefetchesMultiCallReplies(t *testing.T) {
	reg := tools.NewRegistry()
	tool := &runtimeTestTool{name: "file_read", description: "Read file"}
	reg.MustRegister(tool)
	provider := &runtimeTestProvider{
		results: []*GenerateResult{
			{ToolCalls: []ToolCall{
				{CallID: "c1", Name: "file_read", Arguments: map[string]any{"path": "a"}},
				{CallID: "c2", Name: "file_read", ArgumentsError: "arguments are not valid JSON"},
				{CallID: "c3", Name: "file_read", Arguments: map[string]any{"path": "b"}},
			}, FinishReason: FinishToolCalls},
			{ToolCalls: []ToolCall{{CallID: "c4", Name: "file_read", Arguments: map[string]any{"path": "c"}}}, FinishReason: FinishToolCalls},
			{Content: "done", FinishReason: FinishFinal},
		},
	}
	var prefetched [][]string
	store := newRuntimeTestStorage()
	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          reg,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		PrefetchTools: func(_ context.Context, calls []ToolCall) {
			var ids []string
			for _, c := range calls {
				ids = append(ids, c.CallID)
			}
			prefetched = append(prefetched, ids)
		},
	}).Run(context.Background(), RunInput{RunID: "run-prefetch", AgentID: "agent-1", Instruction: "read"})
	if err != nil || result == nil || result.Output != "done" {
		t.Fatalf("expected the run to finish, got %#v, %v", result, err)
	}
	// Only the multi-call reply, without the call whose arguments did not decode.
	if len(prefetched) != 1 || strings.Join(prefetched[0], ",") != "c1,c3" {
		t.Fatalf("expected one prefetch of c1,c3, got %v", prefetched)
	}
	if len(tool.calls) != 3 {
		t.Fatalf("expected every runnable call to run through the registry, got %d", len(tool.calls))
	}
	// The calls of the multi-call reply share a batch; the single call has none.
	var batches []string
	for _, m := range store.messages["run-prefetch"] {
		if m.Role == "assistant" && m.ToolCall != nil {
			batch, _ := m.Metadata[ToolBatchMetaKey].(string)
			batches = append(batches, m.ToolCall.CallID+"="+batch)
		}
	}
	if got := strings.Join(batches, ","); got != "c1=c1,c2=c1,c3=c1,c4=" {
		t.Fatalf("unexpected batches %s", got)
	}
}
