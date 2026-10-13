package agentcore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/inhere/suga/pkg/agentcore/tools"
)

func TestRuntime_Run_FinalAnswer(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done",
			FinishReason: FinishFinal,
			Usage:        Usage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3},
		}},
	}

	rt := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools.NewRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
	})

	result, err := rt.Run(context.Background(), RunInput{
		AgentID:     "agent-1",
		Instruction: "say done",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Status != RunCompleted || result.Output != "done" {
		t.Fatalf("expected completed result with output done, got %#v", result)
	}
	if result.RunID == "" {
		t.Fatal("expected runtime to generate a run id")
	}
	if result.Steps != 1 {
		t.Fatalf("expected one completed step, got %d", result.Steps)
	}

	if len(provider.calls) != 1 {
		t.Fatalf("expected one provider call, got %d", len(provider.calls))
	}
	if provider.calls[0].System != "system" {
		t.Fatalf("expected system prompt to be built, got %#v", provider.calls[0])
	}
	if len(provider.calls[0].Messages) != 1 || provider.calls[0].Messages[0].Content != "say done" {
		t.Fatalf("expected provider to receive the appended user message, got %#v", provider.calls[0].Messages)
	}

	saved := store.runs[result.RunID]
	if saved == nil || saved.Status != RunCompleted {
		t.Fatalf("expected completed state to be saved, got %#v", saved)
	}
	if len(store.messages[result.RunID]) != 2 {
		t.Fatalf("expected persisted message log to include user and assistant messages, got %#v", store.messages[result.RunID])
	}
	if store.messages[result.RunID][1].Role != "assistant" || store.messages[result.RunID][1].Content != "done" {
		t.Fatalf("expected assistant final answer to be appended, got %#v", store.messages[result.RunID])
	}
}

func TestRuntime_Run_UsesGenerateWhenProviderDoesNotStream(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done",
			FinishReason: FinishFinal,
		}},
	}

	rt := New(Config{Provider: provider, Storage: store})

	result, err := rt.Run(context.Background(), RunInput{
		RunID:       "run-non-stream",
		AgentID:     "agent-1",
		Instruction: "hello",
		Stream: func(context.Context, StreamEvent) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Output != "done" {
		t.Fatalf("expected final output done, got %#v", result)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected generate path to be used once, got %d calls", len(provider.calls))
	}
}

func TestRuntime_Run_UsesGenerateWithoutStreamHandler(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestStreamingProvider{
		runtimeTestProvider: runtimeTestProvider{
			results: []*GenerateResult{{
				Content:      "done",
				FinishReason: FinishFinal,
			}},
		},
	}

	rt := New(Config{Provider: provider, Storage: store})

	result, err := rt.Run(context.Background(), RunInput{
		RunID:       "run-generate-fallback",
		AgentID:     "agent-1",
		Instruction: "hello",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Output != "done" {
		t.Fatalf("expected final output done, got %#v", result)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected one provider call, got %d", len(provider.calls))
	}
	if provider.streamCalls != 0 {
		t.Fatalf("expected stream path to stay unused, got %d calls", provider.streamCalls)
	}
}

func TestRuntime_Run_StreamsTextDeltasAndReturnsFinalResult(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestStreamingProvider{
		runtimeTestProvider: runtimeTestProvider{
			results: []*GenerateResult{{
				Content:      "hello",
				FinishReason: FinishFinal,
			}},
		},
		streamEvents: []StreamEvent{
			{Kind: StreamEventTextDelta, Delta: "hel"},
			{Kind: StreamEventTextDelta, Delta: "lo"},
			{Kind: StreamEventMessageStop},
		},
	}

	var seen []StreamEvent
	rt := New(Config{Provider: provider, Storage: store})

	result, err := rt.Run(context.Background(), RunInput{
		RunID:       "run-stream",
		AgentID:     "agent-1",
		Instruction: "hello",
		Stream: func(_ context.Context, event StreamEvent) error {
			seen = append(seen, event)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Output != "hello" {
		t.Fatalf("expected final output hello, got %#v", result)
	}
	if provider.streamCalls != 1 {
		t.Fatalf("expected one stream call, got %d", provider.streamCalls)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected one provider request, got %d", len(provider.calls))
	}
	if len(seen) != 3 {
		t.Fatalf("expected 3 stream events, got %#v", seen)
	}
}

func TestRuntime_Run_StreamHandlerErrorFailsRun(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestStreamingProvider{
		runtimeTestProvider: runtimeTestProvider{
			results: []*GenerateResult{{
				Content:      "hello",
				FinishReason: FinishFinal,
			}},
		},
		streamEvents: []StreamEvent{
			{Kind: StreamEventTextDelta, Delta: "hel"},
		},
	}

	streamErr := errors.New("stream handler failed")
	rt := New(Config{Provider: provider, Storage: store})

	_, err := rt.Run(context.Background(), RunInput{
		RunID:       "run-stream-error",
		AgentID:     "agent-1",
		Instruction: "hello",
		Stream: func(context.Context, StreamEvent) error {
			return streamErr
		},
	})
	if !errors.Is(err, streamErr) {
		t.Fatalf("expected stream handler error, got %v", err)
	}
}

func TestRuntime_Run_StreamingStillPersistsOnlyFinalAssistantMessage(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestStreamingProvider{
		runtimeTestProvider: runtimeTestProvider{
			results: []*GenerateResult{{
				Content:      "hello",
				FinishReason: FinishFinal,
			}},
		},
		streamEvents: []StreamEvent{
			{Kind: StreamEventTextDelta, Delta: "hel"},
			{Kind: StreamEventTextDelta, Delta: "lo"},
			{Kind: StreamEventMessageStop},
		},
	}

	rt := New(Config{Provider: provider, Storage: store})

	result, err := rt.Run(context.Background(), RunInput{
		RunID:       "run-stream-persist",
		AgentID:     "agent-1",
		Instruction: "hello",
		Stream: func(context.Context, StreamEvent) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	msgs := store.messages[result.RunID]
	if len(msgs) != 2 {
		t.Fatalf("expected only user and final assistant messages to persist, got %#v", msgs)
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "hello" {
		t.Fatalf("expected final assistant message to persist, got %#v", msgs[1])
	}
}

func TestRuntime_New_WithOnlyProvider_UsesDefaults(t *testing.T) {
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "ok",
			FinishReason: FinishFinal,
		}},
	}

	rt := New(Config{Provider: provider})

	result, err := rt.Run(context.Background(), RunInput{
		AgentID:     "agent-1",
		Instruction: "hello defaults",
	})
	if err != nil {
		t.Fatalf("expected provider-only runtime to be runnable, got %v", err)
	}
	if result.Status != RunCompleted || result.Output != "ok" {
		t.Fatalf("expected completed result from defaulted runtime, got %#v", result)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected one provider call, got %d", len(provider.calls))
	}
	if provider.calls[0].System != "" {
		t.Fatalf("expected default static prompt builder to produce an empty prompt, got %#v", provider.calls[0])
	}
	if len(provider.calls[0].Messages) != 1 || provider.calls[0].Messages[0].Content != "hello defaults" {
		t.Fatalf("expected default context builder to include the user input, got %#v", provider.calls[0].Messages)
	}
	if len(provider.calls[0].Tools) != 0 {
		t.Fatalf("expected default registry to provide no tools, got %#v", provider.calls[0].Tools)
	}

	_, err = rt.Run(context.Background(), RunInput{
		RunID:      result.RunID,
		ResumeMode: ResumeFromState,
	})
	if !errors.Is(err, ErrRunNotResumable) {
		t.Fatalf("expected default storage to persist the completed run and reject resume, got %v", err)
	}
}

func TestRuntime_New_WithOnlyProvider_RejectsUnsafeRunID(t *testing.T) {
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "ok",
			FinishReason: FinishFinal,
		}},
	}

	rt := New(Config{Provider: provider})

	_, err := rt.Run(context.Background(), RunInput{
		RunID:       filepath.Join("..", "unsafe-run"),
		AgentID:     "agent-1",
		Instruction: "unsafe",
		ResumeMode:  ResumeFromState,
	})

	var storageErr *StorageError
	if !errors.As(err, &storageErr) {
		t.Fatalf("expected unsafe run id to fail with storage error, got %T %v", err, err)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("expected provider not to be called for unsafe run id, got %#v", provider.calls)
	}
}

func TestRuntime_Run_RunIDNotFoundTreatsInputAsNewRun(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "fresh",
			FinishReason: FinishFinal,
		}},
	}
	rt := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools.NewRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
	})

	result, err := rt.Run(context.Background(), RunInput{
		RunID:       "existing-id",
		AgentID:     "agent-1",
		Instruction: "start anyway",
		ResumeMode:  ResumeFromState,
	})
	if err != nil {
		t.Fatalf("expected missing run to be treated as a new run, got %v", err)
	}
	if result.RunID != "existing-id" {
		t.Fatalf("expected provided run id to be reused for the new run, got %#v", result)
	}
	if len(provider.calls) != 1 || len(provider.calls[0].Messages) != 1 || provider.calls[0].Messages[0].Content != "start anyway" {
		t.Fatalf("expected new-run instruction to be appended, got %#v", provider.calls)
	}
}

func TestRuntime_Run_RunIDStorageMissErrorTreatsInputAsNewRun(t *testing.T) {
	tests := []struct {
		name       string
		loadRunErr error
	}{
		{
			name:       "bare os err not exist",
			loadRunErr: os.ErrNotExist,
		},
		{
			name:       "wrapped storage err not exist",
			loadRunErr: &StorageError{Err: fmt.Errorf("load run: %w", os.ErrNotExist)},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newRuntimeTestStorage()
			store.loadRunErr = tc.loadRunErr
			provider := &runtimeTestProvider{
				results: []*GenerateResult{{
					Content:      "fresh",
					FinishReason: FinishFinal,
				}},
			}

			rt := New(Config{
				Provider:       provider,
				Storage:        store,
				Tools:          tools.NewRegistry(),
				PromptBuilder:  runtimeTestPromptBuilder{},
				ContextBuilder: runtimeTestContextBuilder{},
			})

			result, err := rt.Run(context.Background(), RunInput{
				RunID:       "existing-id",
				AgentID:     "agent-1",
				Instruction: "start anyway",
				ResumeMode:  ResumeFromState,
			})
			if err != nil {
				t.Fatalf("expected storage miss to be treated as a new run, got %v", err)
			}
			if result.RunID != "existing-id" {
				t.Fatalf("expected provided run id to be reused for the new run, got %#v", result)
			}
			if len(provider.calls) != 1 || len(provider.calls[0].Messages) != 1 || provider.calls[0].Messages[0].Content != "start anyway" {
				t.Fatalf("expected new-run instruction to be appended, got %#v", provider.calls)
			}
		})
	}
}
