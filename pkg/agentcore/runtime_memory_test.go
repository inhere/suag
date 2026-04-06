package agentcore

import (
	"context"
	"errors"
	"testing"
)

type runtimeTestMemory struct {
	hydrateCalls [][]Message
	observeCalls [][]Message
	buildCalls   int
	context      []Message
	hydrateErr   error
	observeErr   error
	buildErr     error
}

func (m *runtimeTestMemory) Hydrate(_ context.Context, state *RunState) error {
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

func (m *runtimeTestMemory) ObserveMessages(_ context.Context, _ *RunState, messages ...Message) error {
	if m.observeErr != nil {
		return m.observeErr
	}
	m.observeCalls = append(m.observeCalls, cloneMessages(messages))
	return nil
}

func (m *runtimeTestMemory) BuildContext(_ context.Context, _ *RunState) ([]Message, error) {
	if m.buildErr != nil {
		return nil, m.buildErr
	}
	m.buildCalls++
	return cloneMessages(m.context), nil
}

func TestRuntime_Run_DefaultContextUsesMemoryAndObservesMessages(t *testing.T) {
	store := newRuntimeTestStorage()
	mem := &runtimeTestMemory{
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
		Memory:        mem,
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

	mem := &runtimeTestMemory{
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
		Memory:        mem,
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

func TestRuntime_Run_HydrateFailureReturnsMemoryErrorAndSkipsProvider(t *testing.T) {
	store := newRuntimeTestStorage()
	store.runs["run-memory-hydrate-error"] = &RunState{
		RunID:   "run-memory-hydrate-error",
		AgentID: "agent-1",
		Status:  RunYielded,
	}
	store.messages["run-memory-hydrate-error"] = []Message{
		{Role: "user", Content: "persisted"},
	}
	mem := &runtimeTestMemory{
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
		Memory:        mem,
		PromptBuilder: runtimeTestPromptBuilder{prompt: "system"},
	}).Run(context.Background(), RunInput{
		RunID:      "run-memory-hydrate-error",
		ResumeMode: ResumeFromState,
	})
	if err == nil {
		t.Fatal("expected hydrate failure, got nil")
	}
	var memoryErr *MemoryError
	if !errors.As(err, &memoryErr) {
		t.Fatalf("expected memory error wrapper, got %T %v", err, err)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("expected provider to be skipped on hydrate failure, got %#v", provider.calls)
	}
}

func TestRuntime_Run_DefaultContextBuildFailureReturnsMemoryError(t *testing.T) {
	store := newRuntimeTestStorage()
	mem := &runtimeTestMemory{
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
		Memory:        mem,
		PromptBuilder: runtimeTestPromptBuilder{prompt: "system"},
	}).Run(context.Background(), RunInput{
		RunID:       "run-memory-build-error",
		AgentID:     "agent-1",
		Instruction: "hello",
	})
	if err == nil {
		t.Fatal("expected context build failure, got nil")
	}
	var memoryErr *MemoryError
	if !errors.As(err, &memoryErr) {
		t.Fatalf("expected memory error wrapper, got %T %v", err, err)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("expected provider to be skipped on memory build failure, got %#v", provider.calls)
	}
}
