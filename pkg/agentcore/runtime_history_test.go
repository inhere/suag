package agentcore

import (
	"context"
	"errors"
	"testing"

	"github.com/inhere/suga/pkg/agentcore/tools"
)

type runtimeTestHistory struct {
	hydrateCalls [][]Message
	observeCalls [][]Message
	observeCount int
	buildCalls   int
	context      []Message
	hydrateErr   error
	observeErr   error
	observeErrOn int
	buildErr     error
}

func (m *runtimeTestHistory) Hydrate(_ context.Context, state *RunState) error {
	if m.hydrateErr != nil {
		return m.hydrateErr
	}
	if state == nil {
		m.hydrateCalls = append(m.hydrateCalls, nil)
		return nil
	}
	m.hydrateCalls = append(m.hydrateCalls, cloneMessages(state.Messages))
	return nil
}

func (m *runtimeTestHistory) ObserveMessages(_ context.Context, _ *RunState, messages ...Message) error {
	m.observeCount++
	if m.observeErr != nil && m.observeErrOn > 0 && m.observeCount == m.observeErrOn {
		return m.observeErr
	}
	if m.observeErr != nil && m.observeErrOn == 0 {
		return m.observeErr
	}
	m.observeCalls = append(m.observeCalls, cloneMessages(messages))
	return nil
}

func (m *runtimeTestHistory) BuildContext(_ context.Context, _ *RunState) ([]Message, error) {
	if m.buildErr != nil {
		return nil, m.buildErr
	}
	m.buildCalls++
	return cloneMessages(m.context), nil
}

func TestRuntime_Run_DefaultContextUsesMemoryAndObservesMessages(t *testing.T) {
	store := newRuntimeTestStorage()
	mem := &runtimeTestHistory{
		context: []Message{
			{Role: "system", Content: "[Memory summary]: summarized context"},
			{Role: "user", Content: "hello from memory"},
		},
	}
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "final from runtime",
			FinishReason: FinishFinal,
		}},
	}

	result, err := New(Config{
		Provider:      provider,
		Storage:       store,
		History:       mem,
		PromptBuilder: runtimeTestPromptBuilder{prompt: "system"},
	}).Run(context.Background(), RunInput{
		RunID:       "run-memory-default-context",
		AgentID:     "agent-1",
		Instruction: "hello from input",
	})
	if err != nil {
		t.Fatalf("expected runtime to complete, got %v", err)
	}
	if result.Status != RunCompleted || result.Output != "final from runtime" {
		t.Fatalf("unexpected run result: %#v", result)
	}
	if mem.buildCalls != 1 {
		t.Fatalf("memory BuildContext calls = %d, want 1", mem.buildCalls)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(provider.calls))
	}
	if len(provider.calls[0].Messages) != 2 {
		t.Fatalf("provider messages = %#v, want memory-built context", provider.calls[0].Messages)
	}
	if provider.calls[0].Messages[0].Content != "[Memory summary]: summarized context" {
		t.Fatalf("first provider message = %#v, want memory summary", provider.calls[0].Messages[0])
	}
	if len(mem.observeCalls) != 2 {
		t.Fatalf("observe calls = %d, want user + assistant commits", len(mem.observeCalls))
	}
	if len(mem.observeCalls[0]) != 1 || mem.observeCalls[0][0].Role != "user" || mem.observeCalls[0][0].Content != "hello from input" {
		t.Fatalf("first observe call = %#v, want input message commit", mem.observeCalls[0])
	}
	if len(mem.observeCalls[1]) != 1 || mem.observeCalls[1][0].Role != "assistant" || mem.observeCalls[1][0].Content != "final from runtime" {
		t.Fatalf("second observe call = %#v, want final assistant commit", mem.observeCalls[1])
	}
}

func TestRuntime_Run_ResumeHydratesMemoryBeforeBuildingContext(t *testing.T) {
	store := newRuntimeTestStorage()
	store.runs["run-memory-resume"] = &RunState{
		RunID:   "run-memory-resume",
		AgentID: "agent-1",
		Status:  RunYielded,
		Step:    1,
	}
	store.messages["run-memory-resume"] = []Message{
		{Role: "user", Content: "persisted question"},
		{Role: "assistant", Content: "persisted draft"},
	}

	mem := &runtimeTestHistory{
		context: []Message{
			{Role: "system", Content: "[Memory summary]: resumed summary"},
			{Role: "user", Content: "persisted question"},
		},
	}
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "resumed final",
			FinishReason: FinishFinal,
		}},
	}

	result, err := New(Config{
		Provider:      provider,
		Storage:       store,
		History:       mem,
		PromptBuilder: runtimeTestPromptBuilder{prompt: "system"},
	}).Run(context.Background(), RunInput{
		RunID:      "run-memory-resume",
		ResumeMode: ResumeFromState,
	})
	if err != nil {
		t.Fatalf("expected resumed runtime to complete, got %v", err)
	}
	if result.Status != RunCompleted || result.Output != "resumed final" {
		t.Fatalf("unexpected resumed result: %#v", result)
	}
	if len(mem.hydrateCalls) != 1 {
		t.Fatalf("hydrate calls = %d, want 1", len(mem.hydrateCalls))
	}
	if len(mem.hydrateCalls[0]) != 2 || mem.hydrateCalls[0][0].Content != "persisted question" {
		t.Fatalf("hydrate payload = %#v, want persisted transcript", mem.hydrateCalls[0])
	}
	if len(provider.calls) != 1 || len(provider.calls[0].Messages) == 0 {
		t.Fatalf("provider calls = %#v, want resumed context", provider.calls)
	}
	if provider.calls[0].Messages[0].Content != "[Memory summary]: resumed summary" {
		t.Fatalf("provider first message = %#v, want hydrated memory context", provider.calls[0].Messages[0])
	}
}

func TestRuntime_Run_HydrateFailureReturnsHistoryErrorAndSkipsProvider(t *testing.T) {
	store := newRuntimeTestStorage()
	store.runs["run-memory-hydrate-error"] = &RunState{
		RunID:   "run-memory-hydrate-error",
		AgentID: "agent-1",
		Status:  RunYielded,
	}
	store.messages["run-memory-hydrate-error"] = []Message{
		{Role: "user", Content: "persisted"},
	}
	mem := &runtimeTestHistory{
		hydrateErr: errors.New("hydrate failed"),
	}
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "should not run",
			FinishReason: FinishFinal,
		}},
	}

	_, err := New(Config{
		Provider:      provider,
		Storage:       store,
		History:       mem,
		PromptBuilder: runtimeTestPromptBuilder{prompt: "system"},
	}).Run(context.Background(), RunInput{
		RunID:      "run-memory-hydrate-error",
		ResumeMode: ResumeFromState,
	})
	if err == nil {
		t.Fatal("expected hydrate failure, got nil")
	}
	var memoryErr *HistoryError
	if !errors.As(err, &memoryErr) {
		t.Fatalf("expected memory error wrapper, got %T %v", err, err)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("expected provider to be skipped on hydrate failure, got %#v", provider.calls)
	}
}

func TestRuntime_Run_DefaultContextBuildFailureReturnsHistoryError(t *testing.T) {
	store := newRuntimeTestStorage()
	mem := &runtimeTestHistory{
		buildErr: errors.New("build context failed"),
	}
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "should not run",
			FinishReason: FinishFinal,
		}},
	}

	_, err := New(Config{
		Provider:      provider,
		Storage:       store,
		History:       mem,
		PromptBuilder: runtimeTestPromptBuilder{prompt: "system"},
	}).Run(context.Background(), RunInput{
		RunID:       "run-memory-build-error",
		AgentID:     "agent-1",
		Instruction: "hello",
	})
	if err == nil {
		t.Fatal("expected context build failure, got nil")
	}
	var memoryErr *HistoryError
	if !errors.As(err, &memoryErr) {
		t.Fatalf("expected memory error wrapper, got %T %v", err, err)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("expected provider to be skipped on memory build failure, got %#v", provider.calls)
	}
}

func TestRuntime_Run_PersistInputObserveFailureKeepsAuthorityAligned(t *testing.T) {
	store := newRuntimeTestStorage()
	mem := &runtimeTestHistory{
		observeErr:   errors.New("observe failed after input append"),
		observeErrOn: 1,
	}
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "should not run",
			FinishReason: FinishFinal,
		}},
	}

	_, err := New(Config{
		Provider:      provider,
		Storage:       store,
		History:       mem,
		PromptBuilder: runtimeTestPromptBuilder{prompt: "system"},
	}).Run(context.Background(), RunInput{
		RunID:       "run-memory-observe-input-fail",
		AgentID:     "agent-1",
		Instruction: "input that should remain authoritative",
	})
	if err == nil {
		t.Fatal("expected observe failure during input persistence, got nil")
	}
	var memoryErr *HistoryError
	if !errors.As(err, &memoryErr) {
		t.Fatalf("expected memory error wrapper, got %T %v", err, err)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("expected provider not to run after input observe failure, got %#v", provider.calls)
	}

	saved := store.runs["run-memory-observe-input-fail"]
	if saved == nil || saved.Status != RunFailed {
		t.Fatalf("expected failed state after input observe failure, got %#v", saved)
	}
	if len(saved.Messages) != 1 || saved.Messages[0].Role != "user" || saved.Messages[0].Content != "input that should remain authoritative" {
		t.Fatalf("expected failed snapshot to keep authority input message, got %#v", saved.Messages)
	}
	if len(store.messages["run-memory-observe-input-fail"]) != 1 || store.messages["run-memory-observe-input-fail"][0].Content != "input that should remain authoritative" {
		t.Fatalf("expected authority log to retain appended user input, got %#v", store.messages["run-memory-observe-input-fail"])
	}
}

func TestRuntime_Run_PersistOutputObserveFailureKeepsAuthorityAligned(t *testing.T) {
	store := newRuntimeTestStorage()
	mem := &runtimeTestHistory{
		observeErr:   errors.New("observe failed after final output append"),
		observeErrOn: 2,
	}
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "final answer that should remain authoritative",
			FinishReason: FinishFinal,
			Artifacts: []Artifact{{
				Kind: "file",
				URI:  "artifact://final-authority.txt",
			}},
			Metadata: map[string]any{
				"provider": "mock",
				"reason":   "authority",
			},
		}},
	}

	_, err := New(Config{
		Provider:      provider,
		Storage:       store,
		History:       mem,
		PromptBuilder: runtimeTestPromptBuilder{prompt: "system"},
	}).Run(context.Background(), RunInput{
		RunID:       "run-memory-observe-output-fail",
		AgentID:     "agent-1",
		Instruction: "produce final output",
	})
	if err == nil {
		t.Fatal("expected observe failure during output persistence, got nil")
	}
	var memoryErr *HistoryError
	if !errors.As(err, &memoryErr) {
		t.Fatalf("expected memory error wrapper, got %T %v", err, err)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected provider to run before output observe failure, got %#v", provider.calls)
	}

	saved := store.runs["run-memory-observe-output-fail"]
	if saved == nil || saved.Status != RunFailed {
		t.Fatalf("expected failed state after output observe failure, got %#v", saved)
	}
	if len(saved.Messages) != 2 || saved.Messages[1].Role != "assistant" || saved.Messages[1].Content != "final answer that should remain authoritative" {
		t.Fatalf("expected failed snapshot to keep final authoritative answer, got %#v", saved.Messages)
	}
	if saved.Messages[1].Metadata["provider"] != "mock" || saved.Messages[1].Metadata["reason"] != "authority" {
		t.Fatalf("expected failed snapshot to keep final message metadata, got %#v", saved.Messages[1].Metadata)
	}
	if len(saved.Artifacts) != 1 || saved.Artifacts[0].URI != "artifact://final-authority.txt" {
		t.Fatalf("expected failed snapshot to keep final artifacts, got %#v", saved)
	}
	if len(store.messages["run-memory-observe-output-fail"]) != 2 || store.messages["run-memory-observe-output-fail"][1].Content != "final answer that should remain authoritative" {
		t.Fatalf("expected authority log to keep final assistant output, got %#v", store.messages["run-memory-observe-output-fail"])
	}
	if store.messages["run-memory-observe-output-fail"][1].Metadata["provider"] != "mock" || store.messages["run-memory-observe-output-fail"][1].Metadata["reason"] != "authority" {
		t.Fatalf("expected authority log to keep final message metadata, got %#v", store.messages["run-memory-observe-output-fail"][1].Metadata)
	}
}

func TestRuntime_Run_PersistToolOutputObserveFailureKeepsToolTranscriptAuthoritative(t *testing.T) {
	store := newRuntimeTestStorage()
	mem := &runtimeTestHistory{
		observeErr:   errors.New("observe failed after tool transcript append"),
		observeErrOn: 2,
	}
	reg := tools.NewRegistry()
	reg.MustRegister(&runtimeTestTool{
		name: "lookup",
		result: &ToolResult{
			Content: "tool-output",
			Artifacts: []Artifact{{
				Kind: "file",
				URI:  "artifact://report.txt",
			}},
			Metadata: map[string]any{
				"source": "tool",
			},
		},
	})
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID: "call-1",
				Name:   "lookup",
			}},
			FinishReason: FinishToolCalls,
		}},
	}

	_, err := New(Config{
		Provider:      provider,
		Storage:       store,
		History:       mem,
		Tools:         reg,
		PromptBuilder: runtimeTestPromptBuilder{prompt: "system"},
	}).Run(context.Background(), RunInput{
		RunID:       "run-memory-observe-tool-fail",
		AgentID:     "agent-1",
		Instruction: "collect tool facts",
	})
	if err == nil {
		t.Fatal("expected observe failure during tool transcript persistence, got nil")
	}
	var memoryErr *HistoryError
	if !errors.As(err, &memoryErr) {
		t.Fatalf("expected memory error wrapper, got %T %v", err, err)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected provider to run once before tool transcript observe failure, got %#v", provider.calls)
	}

	saved := store.runs["run-memory-observe-tool-fail"]
	if saved == nil || saved.Status != RunFailed {
		t.Fatalf("expected failed state after tool transcript observe failure, got %#v", saved)
	}
	if len(saved.Messages) != 3 {
		t.Fatalf("expected failed snapshot to keep user, assistant tool-call, and tool result, got %#v", saved.Messages)
	}
	if len(saved.Artifacts) != 1 || saved.Artifacts[0].URI != "artifact://report.txt" {
		t.Fatalf("expected failed snapshot to keep tool artifacts, got %#v", saved)
	}

	logged := store.messages["run-memory-observe-tool-fail"]
	if len(logged) != 3 {
		t.Fatalf("expected authority log to keep full tool transcript, got %#v", logged)
	}
	if logged[1].Role != "assistant" || logged[1].ToolCall == nil || logged[1].ToolCall.Name != "lookup" {
		t.Fatalf("expected assistant tool-call to remain authoritative, got %#v", logged[1])
	}
	if logged[2].ToolResult == nil || logged[2].ToolResult.Content != "tool-output" {
		t.Fatalf("expected tool result content to remain authoritative, got %#v", logged[2])
	}
	if logged[2].ToolResult.Metadata["source"] != "tool" {
		t.Fatalf("expected tool result metadata to remain authoritative, got %#v", logged[2].ToolResult)
	}
	if len(logged[2].ToolResult.Artifacts) != 1 || logged[2].ToolResult.Artifacts[0].URI != "artifact://report.txt" {
		t.Fatalf("expected tool result artifacts to remain authoritative, got %#v", logged[2].ToolResult)
	}
}

func TestRuntime_Run_PersistMutatedToolOutputObserveFailureKeepsMutatedFactsAuthoritative(t *testing.T) {
	store := newRuntimeTestStorage()
	mem := &runtimeTestHistory{
		observeErr:   errors.New("observe failed after mutated tool transcript append"),
		observeErrOn: 2,
	}
	reg := tools.NewRegistry()
	reg.MustRegister(&runtimeTestTool{
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

	_, err := New(Config{
		Provider:      provider,
		Storage:       store,
		History:       mem,
		Tools:         reg,
		PromptBuilder: runtimeTestPromptBuilder{prompt: "system"},
		Hooks:         []Hook{hook},
	}).Run(context.Background(), RunInput{
		RunID:       "run-memory-observe-mutated-tool-fail",
		AgentID:     "agent-1",
		Instruction: "collect mutated tool facts",
	})
	if err == nil {
		t.Fatal("expected observe failure during mutated tool transcript persistence, got nil")
	}
	var memoryErr *HistoryError
	if !errors.As(err, &memoryErr) {
		t.Fatalf("expected memory error wrapper, got %T %v", err, err)
	}

	saved := store.runs["run-memory-observe-mutated-tool-fail"]
	if saved == nil || saved.Status != RunFailed {
		t.Fatalf("expected failed state after mutated tool transcript observe failure, got %#v", saved)
	}
	if len(saved.Messages) != 3 {
		t.Fatalf("expected failed snapshot to keep mutated assistant tool-call and tool result, got %#v", saved.Messages)
	}
	if saved.Messages[1].ToolCall == nil || saved.Messages[1].ToolCall.Name != "lookup-mutated" || saved.Messages[1].ToolCall.CallID != "call-1-mutated" {
		t.Fatalf("expected failed snapshot to keep mutated assistant tool-call, got %#v", saved.Messages[1])
	}
	if saved.Messages[2].ToolResult == nil || saved.Messages[2].ToolResult.Content != "mutated-output" {
		t.Fatalf("expected failed snapshot to keep mutated tool result content, got %#v", saved.Messages[2])
	}
	if saved.Messages[2].ToolResult.Metadata["source"] != "after_tool" {
		t.Fatalf("expected failed snapshot to keep mutated tool result metadata, got %#v", saved.Messages[2].ToolResult)
	}
	if len(saved.Artifacts) != 1 || saved.Artifacts[0].URI != "artifact://mutated.txt" {
		t.Fatalf("expected failed snapshot to keep mutated artifacts, got %#v", saved)
	}

	logged := store.messages["run-memory-observe-mutated-tool-fail"]
	if len(logged) != 3 {
		t.Fatalf("expected authority log to keep mutated tool transcript, got %#v", logged)
	}
	if logged[1].ToolCall == nil || logged[1].ToolCall.Name != "lookup-mutated" || logged[1].ToolCall.CallID != "call-1-mutated" {
		t.Fatalf("expected authority log to keep mutated assistant tool-call, got %#v", logged[1])
	}
	if logged[2].ToolResult == nil || logged[2].ToolResult.Content != "mutated-output" {
		t.Fatalf("expected authority log to keep mutated tool result content, got %#v", logged[2])
	}
	if logged[2].ToolResult.Metadata["source"] != "after_tool" {
		t.Fatalf("expected authority log to keep mutated tool result metadata, got %#v", logged[2].ToolResult)
	}
	if len(logged[2].ToolResult.Artifacts) != 1 || logged[2].ToolResult.Artifacts[0].URI != "artifact://mutated.txt" {
		t.Fatalf("expected authority log to keep mutated tool artifacts, got %#v", logged[2].ToolResult)
	}
}
