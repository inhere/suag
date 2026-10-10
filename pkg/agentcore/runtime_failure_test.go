package agentcore

import (
	"context"
	"errors"
	"github.com/inhere/suag/pkg/agentcore/tools"
	"strings"
	"testing"
)

func TestRuntime_Run_PersistOutputFailureOnErrorYieldDoesNotForkAuthorityState(t *testing.T) {
	store := newRuntimeTestStorage()
	store.appendErr = errors.New("append failed")
	store.appendErrOnCall = 2
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done",
			FinishReason: FinishFinal,
		}},
	}
	hook := &runtimeTestHook{
		onError: func(ctrl *Control) {
			ctrl.Yield("recover_after_persist_output", map[string]any{"stage": "on_error"})
		},
	}

	rt := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools.NewRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	})

	result, err := rt.Run(context.Background(), RunInput{
		RunID:       "run-persist-output-yield",
		AgentID:     "agent-1",
		Instruction: "trigger append failure",
	})
	if err != nil {
		t.Fatalf("expected on-error yield to recover persist-output failure, got %v", err)
	}
	if result.Status != RunYielded || result.Yield == nil || result.Yield.Reason != "recover_after_persist_output" {
		t.Fatalf("expected yielded result after persist-output failure, got %#v", result)
	}

	saved := store.runs["run-persist-output-yield"]
	if saved == nil || saved.Status != RunYielded {
		t.Fatalf("expected yielded state to be persisted, got %#v", saved)
	}
	if len(saved.Messages) != 1 || saved.Messages[0].Content != "trigger append failure" {
		t.Fatalf("expected state snapshot to avoid unlogged assistant message, got %#v", saved.Messages)
	}
	if len(store.messages["run-persist-output-yield"]) != 1 || store.messages["run-persist-output-yield"][0].Content != "trigger append failure" {
		t.Fatalf("expected authority log to remain aligned with state snapshot, got %#v", store.messages["run-persist-output-yield"])
	}
}

func TestRuntime_Run_PersistInputAppendFailureDoesNotForkAuthorityState(t *testing.T) {
	store := newRuntimeTestStorage()
	store.appendErr = errors.New("append input failed")
	store.appendErrOnCall = 1
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "should-not-run",
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

	_, err := rt.Run(context.Background(), RunInput{
		RunID:       "run-persist-input-fail",
		AgentID:     "agent-1",
		Instruction: "input that should roll back",
	})

	var storageErr *StorageError
	if !errors.As(err, &storageErr) {
		t.Fatalf("expected storage error from input append failure, got %T %v", err, err)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("expected provider not to run after persist_input failure, got %#v", provider.calls)
	}

	saved := store.runs["run-persist-input-fail"]
	if saved == nil || saved.Status != RunFailed {
		t.Fatalf("expected failed state to be persisted after input append failure, got %#v", saved)
	}
	if len(saved.Messages) != 0 {
		t.Fatalf("expected snapshot to roll back unlogged input message, got %#v", saved.Messages)
	}
	if len(store.messages["run-persist-input-fail"]) != 0 {
		t.Fatalf("expected authority log to remain empty after input append failure, got %#v", store.messages["run-persist-input-fail"])
	}
}

func TestRuntime_Run_ShortCircuitCompletedAppendFailureDoesNotForkAuthorityState(t *testing.T) {
	store := newRuntimeTestStorage()
	store.appendErr = errors.New("append short-circuit output failed")
	store.appendErrOnCall = 2
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "should-not-run",
			FinishReason: FinishFinal,
		}},
	}
	hook := &runtimeTestHook{
		beforeModel: func(ctrl *Control) {
			ctrl.ShortCircuit(&RunResult{
				Status: RunCompleted,
				Output: "short-circuit answer",
			})
		},
	}

	rt := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools.NewRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	})

	_, err := rt.Run(context.Background(), RunInput{
		RunID:       "run-short-circuit-append-fail",
		AgentID:     "agent-1",
		Instruction: "trigger short circuit",
	})

	var storageErr *StorageError
	if !errors.As(err, &storageErr) {
		t.Fatalf("expected storage error from short-circuit append failure, got %T %v", err, err)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("expected provider not to run after short-circuit, got %#v", provider.calls)
	}

	saved := store.runs["run-short-circuit-append-fail"]
	if saved == nil {
		t.Fatalf("expected snapshot to be saved, got %#v", saved)
	}
	if len(saved.Messages) != 1 || saved.Messages[0].Content != "trigger short circuit" {
		t.Fatalf("expected snapshot to roll back unlogged assistant output, got %#v", saved.Messages)
	}
	if len(store.messages["run-short-circuit-append-fail"]) != 1 || store.messages["run-short-circuit-append-fail"][0].Content != "trigger short circuit" {
		t.Fatalf("expected authority log to keep only the logged user input, got %#v", store.messages["run-short-circuit-append-fail"])
	}
}

func TestRuntime_Run_AfterToolErrorIsNotOverriddenByEarlierControl(t *testing.T) {
	store := newRuntimeTestStorage()
	reg := tools.NewRegistry()
	reg.MustRegister(&runtimeTestTool{
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

	firstHook := &runtimeTestHook{
		afterTool: func(ctrl *Control) {
			ctrl.Yield("stale_after_tool_control", map[string]any{"hook": "first"})
		},
	}
	secondHook := &runtimeTestHook{
		afterToolErr: errors.New("after tool boom"),
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          reg,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{firstHook, secondHook},
	}).Run(context.Background(), RunInput{
		RunID:       "run-after-tool-control-then-error",
		AgentID:     "agent-1",
		Instruction: "execute and fail after control",
	})

	if result != nil {
		t.Fatalf("expected after_tool error to remain terminal, got result=%#v", result)
	}
	var hookErr *HookError
	if !errors.As(err, &hookErr) {
		t.Fatalf("expected after-tool error to surface as hook error, got %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "after tool boom") {
		t.Fatalf("expected real after_tool hook error to survive stale control, got %v", err)
	}
	saved := store.runs["run-after-tool-control-then-error"]
	if saved == nil || saved.Status != RunFailed || !strings.Contains(saved.LastError, "after tool boom") {
		t.Fatalf("expected failed state to keep after_tool root cause, got %#v", saved)
	}
	if len(store.messages["run-after-tool-control-then-error"]) != 3 ||
		store.messages["run-after-tool-control-then-error"][1].Role != "assistant" ||
		store.messages["run-after-tool-control-then-error"][2].Role != "tool" {
		t.Fatalf("expected assistant tool-call and tool messages to be persisted before after_tool error handling, got %#v", store.messages["run-after-tool-control-then-error"])
	}
}

func TestRuntime_Run_PersistToolOutputAppendFailureIsNotRecoverable(t *testing.T) {
	store := newRuntimeTestStorage()
	store.appendErr = errors.New("append tool output failed")
	store.appendErrOnCall = 2

	reg := tools.NewRegistry()
	reg.MustRegister(&runtimeTestTool{
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
		onError: func(ctrl *Control) {
			ctrl.Yield("should_not_recover", map[string]any{"stage": "on_error"})
		},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          reg,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	}).Run(context.Background(), RunInput{
		RunID:       "run-persist-tool-output-fail",
		AgentID:     "agent-1",
		Instruction: "execute tool then fail append",
	})

	if err == nil {
		t.Fatalf("expected persist_tool_output append failure to remain an error, got result=%#v", result)
	}
	var storageErr *StorageError
	if !errors.As(err, &storageErr) {
		t.Fatalf("expected storage error from persist_tool_output append failure, got %T %v", err, err)
	}
	saved := store.runs["run-persist-tool-output-fail"]
	if saved == nil || saved.Status != RunFailed {
		t.Fatalf("expected failed state after persist_tool_output append failure, got %#v", saved)
	}
	if len(saved.Messages) != 3 || saved.Messages[1].Role != "assistant" || saved.Messages[2].Role != "tool" || saved.Messages[2].ToolResult == nil || saved.Messages[2].ToolResult.Content != "tool-output" {
		t.Fatalf("expected failed snapshot to retain executed tool fact, got %#v", saved)
	}
	if len(store.messages["run-persist-tool-output-fail"]) != 1 || store.messages["run-persist-tool-output-fail"][0].Role != "user" {
		t.Fatalf("expected authority log not to expose a recoverable tool-output gap, got %#v", store.messages["run-persist-tool-output-fail"])
	}
}

func TestRuntime_Run_PersistToolOutputAppendFailureAfterRunYieldDoesNotOverrideError(t *testing.T) {
	store := newRuntimeTestStorage()
	store.appendErr = errors.New("append tool output failed")
	store.appendErrOnCall = 2

	reg := tools.NewRegistry()
	reg.MustRegister(&runtimeTestTool{
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
		afterRun: func(ctrl *Control) {
			ctrl.Yield("after_run_should_not_recover", map[string]any{"stage": "after_run"})
		},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          reg,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	}).Run(context.Background(), RunInput{
		RunID:       "run-persist-tool-output-after-run-yield",
		AgentID:     "agent-1",
		Instruction: "execute tool then fail append",
	})

	if result != nil {
		t.Fatalf("expected persist_tool_output failure to keep returning error, got result=%#v", result)
	}
	var storageErr *StorageError
	if !errors.As(err, &storageErr) {
		t.Fatalf("expected storage error to survive after-run yield, got %T %v", err, err)
	}
	saved := store.runs["run-persist-tool-output-after-run-yield"]
	if saved == nil || saved.Status != RunFailed {
		t.Fatalf("expected failed state to remain failed, got %#v", saved)
	}
	if len(store.messages["run-persist-tool-output-after-run-yield"]) != 1 {
		t.Fatalf("expected authority log to remain non-recoverable, got %#v", store.messages["run-persist-tool-output-after-run-yield"])
	}
}

func TestRuntime_Run_PersistToolOutputOnErrorObservedErrorDoesNotOverrideRootCause(t *testing.T) {
	store := newRuntimeTestStorage()
	store.appendErr = errors.New("append tool output failed")
	store.appendErrOnCall = 2

	reg := tools.NewRegistry()
	reg.MustRegister(&runtimeTestTool{
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
		onErrorReturn: func(event *ErrorEvent, _ *Control) error {
			if event != nil && event.Stage == "persist_tool_output" {
				return errors.New("observe boom")
			}
			return nil
		},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          reg,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	}).Run(context.Background(), RunInput{
		RunID:       "run-persist-tool-output-observer-error",
		AgentID:     "agent-1",
		Instruction: "execute tool then fail append and observer",
	})

	if result != nil {
		t.Fatalf("expected persist_tool_output failure to remain terminal, got result=%#v", result)
	}
	var storageErr *StorageError
	if !errors.As(err, &storageErr) {
		t.Fatalf("expected original storage error to survive observer failure, got %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "append tool output failed") {
		t.Fatalf("expected original persist_tool_output root cause to be preserved, got %v", err)
	}
	if strings.Contains(err.Error(), "observe boom") {
		t.Fatalf("expected observer hook failure not to replace storage root cause, got %v", err)
	}
	saved := store.runs["run-persist-tool-output-observer-error"]
	if saved == nil || saved.Status != RunFailed || !strings.Contains(saved.LastError, "append tool output failed") {
		t.Fatalf("expected failed state to keep original storage error, got %#v", saved)
	}
}

func TestRuntime_Run_PersistToolOutputAppendFailureAfterRunErrorDoesNotOverrideRootCause(t *testing.T) {
	store := newRuntimeTestStorage()
	store.appendErr = errors.New("append tool output failed")
	store.appendErrOnCall = 2

	reg := tools.NewRegistry()
	reg.MustRegister(&runtimeTestTool{
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

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          reg,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{afterRunErrorHook(errors.New("after run boom"))},
	}).Run(context.Background(), RunInput{
		RunID:       "run-persist-tool-output-after-run-error",
		AgentID:     "agent-1",
		Instruction: "execute tool then fail append",
	})

	if result != nil {
		t.Fatalf("expected persist_tool_output failure to remain an error, got result=%#v", result)
	}
	var storageErr *StorageError
	if !errors.As(err, &storageErr) {
		t.Fatalf("expected storage error to survive after-run error, got %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "append tool output failed") {
		t.Fatalf("expected original storage root cause to be preserved, got %v", err)
	}
	saved := store.runs["run-persist-tool-output-after-run-error"]
	if saved == nil || saved.Status != RunFailed || !strings.Contains(saved.LastError, "append tool output failed") {
		t.Fatalf("expected failed state to keep original storage error, got %#v", saved)
	}
}

func TestRuntime_Run_ProviderErrorMapped(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		errs: []error{errors.New("provider boom")},
	}

	rt := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools.NewRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
	})

	_, err := rt.Run(context.Background(), RunInput{
		RunID:       "run-provider-fail",
		AgentID:     "agent-1",
		Instruction: "trigger provider",
	})

	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected provider error wrapper, got %T %v", err, err)
	}
	saved := store.runs["run-provider-fail"]
	if saved == nil || saved.Status != RunFailed || saved.LastError == "" {
		t.Fatalf("expected provider failure state to be persisted, got %#v", saved)
	}
}

func TestRuntime_Run_StorageErrorMapped(t *testing.T) {
	store := newRuntimeTestStorage()
	store.saveRunErr = errors.New("disk full")
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done",
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

	_, err := rt.Run(context.Background(), RunInput{
		AgentID:     "agent-1",
		Instruction: "persist this",
	})

	var storageErr *StorageError
	if !errors.As(err, &storageErr) {
		t.Fatalf("expected storage error wrapper, got %T %v", err, err)
	}
}
