package agentcore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/inhere/suag/pkg/agentcore/tools"
)

func TestRuntime_Run_HookYieldReturnsYieldedResult(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "should-not-run",
			FinishReason: FinishFinal,
		}},
	}
	hook := &runtimeTestHook{
		beforeModel: func(ctrl *Control) {
			ctrl.Yield("await_approval", map[string]any{"policy": "manual"})
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
		RunID:       "run-yield",
		AgentID:     "agent-1",
		Instruction: "pause",
	})
	if err != nil {
		t.Fatalf("expected yield to return a result, got %v", err)
	}
	if result.Status != RunYielded || result.Yield == nil || result.Yield.Reason != "await_approval" {
		t.Fatalf("expected yielded result, got %#v", result)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("expected provider not to run after yield, got %#v", provider.calls)
	}
	saved := store.runs["run-yield"]
	if saved == nil || saved.Status != RunYielded || saved.LastError != "" {
		t.Fatalf("expected yielded state to be persisted, got %#v", saved)
	}
	if len(store.messages["run-yield"]) != 1 || store.messages["run-yield"][0].Content != "pause" {
		t.Fatalf("expected only the user message to be persisted on yield, got %#v", store.messages["run-yield"])
	}
}

func TestRuntime_Run_BeforePromptBuildRunsBeforeBuilderAndCanYield(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "should-not-run",
			FinishReason: FinishFinal,
		}},
	}
	var promptCalls int
	hook := &runtimeTestHook{
		beforePrompt: func(ctrl *Control) {
			ctrl.Yield("before_prompt", map[string]any{"stage": "before_prompt"})
		},
	}

	rt := New(Config{
		Provider: provider,
		Storage:  store,
		Tools:    tools.NewRegistry(),
		PromptBuilder: runtimeTestPromptBuilder{
			calls: &promptCalls,
			onBuild: func(*RunState) error {
				return fmt.Errorf("prompt builder should not run after before-prompt yield")
			},
		},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	})

	result, err := rt.Run(context.Background(), RunInput{
		RunID:       "run-before-prompt-yield",
		AgentID:     "agent-1",
		Instruction: "pause before prompt",
	})
	if err != nil {
		t.Fatalf("expected before-prompt yield to succeed, got %v", err)
	}
	if promptCalls != 0 {
		t.Fatalf("expected prompt builder not to run after before-prompt yield, got %d calls", promptCalls)
	}
	if result.Status != RunYielded || result.Yield == nil || result.Yield.Reason != "before_prompt" {
		t.Fatalf("expected yielded result from before-prompt control, got %#v", result)
	}
}

func TestRuntime_Run_BeforeContextBuildRunsBeforeBuilderAndCanShortCircuit(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "should-not-run",
			FinishReason: FinishFinal,
		}},
	}
	var contextCalls int
	hook := &runtimeTestHook{
		beforeContext: func(ctrl *Control) {
			ctrl.ShortCircuit(&RunResult{
				Status: RunCompleted,
				Output: "from-context-hook",
			})
		},
	}

	rt := New(Config{
		Provider:      provider,
		Storage:       store,
		Tools:         tools.NewRegistry(),
		PromptBuilder: runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{
			calls: &contextCalls,
			onBuild: func(*RunState) error {
				return fmt.Errorf("context builder should not run after before-context short-circuit")
			},
		},
		Hooks: []Hook{hook},
	})

	result, err := rt.Run(context.Background(), RunInput{
		RunID:       "run-before-context-short-circuit",
		AgentID:     "agent-1",
		Instruction: "finish before context",
	})
	if err != nil {
		t.Fatalf("expected before-context short-circuit to succeed, got %v", err)
	}
	if contextCalls != 0 {
		t.Fatalf("expected context builder not to run after before-context short-circuit, got %d calls", contextCalls)
	}
	if result.Status != RunCompleted || result.Output != "from-context-hook" {
		t.Fatalf("expected short-circuit result from before-context control, got %#v", result)
	}
}

func TestRuntime_Run_AfterModelYieldPreservesStepAndUsage(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done",
			FinishReason: FinishFinal,
			Usage:        Usage{InputTokens: 11, OutputTokens: 7, TotalTokens: 18},
		}},
	}
	hook := &runtimeTestHook{
		afterModel: func(ctrl *Control) {
			ctrl.Yield("await_review", map[string]any{"stage": "after_model"})
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
		RunID:       "run-after-model-yield",
		AgentID:     "agent-1",
		Instruction: "compute first",
	})
	if err != nil {
		t.Fatalf("expected after-model yield to succeed, got %v", err)
	}
	if result.Status != RunYielded || result.Steps != 1 || result.Usage.TotalTokens != 18 {
		t.Fatalf("expected yielded result with persisted usage and step, got %#v", result)
	}

	saved := store.runs["run-after-model-yield"]
	if saved == nil || saved.Status != RunYielded || saved.Step != 1 || saved.Usage.TotalTokens != 18 {
		t.Fatalf("expected yielded state to preserve step and usage, got %#v", saved)
	}
	if len(store.messages["run-after-model-yield"]) != 1 {
		t.Fatalf("expected only the user message to be persisted on after-model yield, got %#v", store.messages["run-after-model-yield"])
	}
}

func TestRuntime_Run_HookShortCircuitReturnsReplacement(t *testing.T) {
	store := newRuntimeTestStorage()
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
				Output: "cached",
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

	result, err := rt.Run(context.Background(), RunInput{
		RunID:       "run-short-circuit",
		AgentID:     "agent-1",
		Instruction: "use cache",
	})
	if err != nil {
		t.Fatalf("expected short-circuit replacement to succeed, got %v", err)
	}
	if result.Status != RunCompleted || result.Output != "cached" {
		t.Fatalf("expected short-circuit replacement result, got %#v", result)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("expected provider not to run after short-circuit, got %#v", provider.calls)
	}
	saved := store.runs["run-short-circuit"]
	if saved == nil || saved.Status != RunCompleted {
		t.Fatalf("expected completed short-circuit state to be persisted, got %#v", saved)
	}
	if len(store.messages["run-short-circuit"]) != 2 || store.messages["run-short-circuit"][1].Content != "cached" {
		t.Fatalf("expected assistant short-circuit output to be persisted, got %#v", store.messages["run-short-circuit"])
	}
}

func TestRuntime_Run_AfterRunYieldIsNotIgnored(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done",
			FinishReason: FinishFinal,
			Usage:        Usage{InputTokens: 2, OutputTokens: 3, TotalTokens: 5},
		}},
	}
	hook := &runtimeTestHook{
		afterRun: func(ctrl *Control) {
			ctrl.Yield("after_run_pause", map[string]any{"source": "after_run"})
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
		RunID:       "run-after-run-yield",
		AgentID:     "agent-1",
		Instruction: "finish then yield",
	})
	if err != nil {
		t.Fatalf("expected after-run yield to override final return, got %v", err)
	}
	if result.Status != RunYielded || result.Yield == nil || result.Yield.Reason != "after_run_pause" {
		t.Fatalf("expected yielded result from after-run control, got %#v", result)
	}

	saved := store.runs["run-after-run-yield"]
	if saved == nil || saved.Status != RunYielded || saved.Step != 1 || saved.Usage.TotalTokens != 5 {
		t.Fatalf("expected after-run yield to persist yielded state with usage, got %#v", saved)
	}
}

func TestRuntime_Run_ShortCircuitStillTriggersAfterRun(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "should-not-run",
			FinishReason: FinishFinal,
		}},
	}
	afterRunCalled := false
	hook := &runtimeTestHook{
		beforeModel: func(ctrl *Control) {
			ctrl.ShortCircuit(&RunResult{
				Status: RunCompleted,
				Output: "cached",
			})
		},
		afterRunEvent: func(event *RunEvent, _ *Control) {
			afterRunCalled = true
			if event.Result == nil || event.Result.Output != "cached" {
				t.Fatalf("expected after-run to observe short-circuit result, got %#v", event.Result)
			}
		},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools.NewRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	}).Run(context.Background(), RunInput{
		RunID:       "run-short-circuit-after-run",
		AgentID:     "agent-1",
		Instruction: "use cache",
	})
	if err != nil {
		t.Fatalf("expected short-circuit to succeed, got %v", err)
	}
	if result.Status != RunCompleted || result.Output != "cached" {
		t.Fatalf("expected short-circuit result, got %#v", result)
	}
	if !afterRunCalled {
		t.Fatal("expected after-run to be triggered after short-circuit")
	}
}

func TestRuntime_Run_YieldStillTriggersAfterRun(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "should-not-run",
			FinishReason: FinishFinal,
		}},
	}
	afterRunCalled := false
	hook := &runtimeTestHook{
		beforeModel: func(ctrl *Control) {
			ctrl.Yield("pause_before_model", map[string]any{"stage": "before_model"})
		},
		afterRunEvent: func(event *RunEvent, _ *Control) {
			afterRunCalled = true
			if event.Result == nil || event.Result.Status != RunYielded {
				t.Fatalf("expected after-run to observe yielded result, got %#v", event.Result)
			}
		},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools.NewRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	}).Run(context.Background(), RunInput{
		RunID:       "run-yield-after-run",
		AgentID:     "agent-1",
		Instruction: "pause",
	})
	if err != nil {
		t.Fatalf("expected yield to succeed, got %v", err)
	}
	if result.Status != RunYielded || result.Yield == nil || result.Yield.Reason != "pause_before_model" {
		t.Fatalf("expected yielded result, got %#v", result)
	}
	if !afterRunCalled {
		t.Fatal("expected after-run to be triggered after yield")
	}
}

func TestRuntime_Run_HardFailureTriggersOnErrorAndAfterRun(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		errs: []error{errors.New("provider boom")},
	}
	onErrorCalled := false
	afterRunCalled := false
	hook := &runtimeTestHook{
		onErrorEvent: func(event *ErrorEvent, _ *Control) {
			onErrorCalled = true
			if event.Stage != "model" {
				t.Fatalf("expected on-error to observe model stage, got %#v", event)
			}
		},
		afterRunEvent: func(event *RunEvent, _ *Control) {
			afterRunCalled = true
			if event.Result != nil {
				t.Fatalf("expected after-run to observe nil result on hard failure, got %#v", event.Result)
			}
		},
	}

	_, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools.NewRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	}).Run(context.Background(), RunInput{
		RunID:       "run-hard-failure-after-run",
		AgentID:     "agent-1",
		Instruction: "boom",
	})
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected provider error, got %T %v", err, err)
	}
	if !onErrorCalled {
		t.Fatal("expected on-error to be triggered on hard failure")
	}
	if !afterRunCalled {
		t.Fatal("expected after-run to be triggered on hard failure")
	}
}

func TestRuntime_Run_HardFailureAfterRunYieldDoesNotOverrideError(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		errs: []error{errors.New("provider boom")},
	}
	hook := &runtimeTestHook{
		afterRun: func(ctrl *Control) {
			ctrl.Yield("after_run_should_not_recover", map[string]any{"stage": "after_run"})
		},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools.NewRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	}).Run(context.Background(), RunInput{
		RunID:       "run-hard-failure-after-run-yield",
		AgentID:     "agent-1",
		Instruction: "boom",
	})

	if result != nil {
		t.Fatalf("expected hard failure to keep returning error, got result=%#v", result)
	}
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected provider error to survive after-run yield, got %T %v", err, err)
	}
	saved := store.runs["run-hard-failure-after-run-yield"]
	if saved == nil || saved.Status != RunFailed {
		t.Fatalf("expected failed state to remain failed, got %#v", saved)
	}
}

func TestRuntime_Run_HardFailureAfterRunErrorDoesNotOverrideRootCause(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		errs: []error{errors.New("provider boom")},
	}
	hook := &runtimeTestHook{
		afterRunEvent: func(_ *RunEvent, _ *Control) {
			t.Helper()
		},
	}
	hook.afterRun = nil
	hookWithError := &runtimeTestHook{
		afterRunEvent: func(_ *RunEvent, _ *Control) {},
	}
	_ = hook
	_ = hookWithError

	afterRunErrHook := &runtimeTestHook{}
	afterRunErrHook.afterRunEvent = nil
	afterRunErrHook.afterRun = nil

	afterRunErr := errors.New("after run boom")
	customHook := &runtimeTestHook{
		afterRunEvent: nil,
	}
	_ = afterRunErr
	_ = customHook

	afterRunFailureHook := afterRunErrorHook(afterRunErr)

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools.NewRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{afterRunFailureHook},
	}).Run(context.Background(), RunInput{
		RunID:       "run-hard-failure-after-run-error",
		AgentID:     "agent-1",
		Instruction: "boom",
	})

	if result != nil {
		t.Fatalf("expected original hard failure to remain an error, got result=%#v", result)
	}
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected provider error to survive after-run error, got %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "provider boom") {
		t.Fatalf("expected original provider root cause to be preserved, got %v", err)
	}
	saved := store.runs["run-hard-failure-after-run-error"]
	if saved == nil || saved.Status != RunFailed || !strings.Contains(saved.LastError, "provider boom") {
		t.Fatalf("expected failed state to keep original provider error, got %#v", saved)
	}
}

func TestRuntime_Run_HookErrorMapped(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done",
			FinishReason: FinishFinal,
		}},
	}
	hook := &runtimeTestHook{beforeModelErr: errors.New("hook boom")}

	rt := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools.NewRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	})

	_, err := rt.Run(context.Background(), RunInput{
		AgentID:     "agent-1",
		Instruction: "trigger hook",
	})

	var hookErr *HookError
	if !errors.As(err, &hookErr) {
		t.Fatalf("expected hook error wrapper, got %T %v", err, err)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("expected provider not to be called after hook failure, got %#v", provider.calls)
	}
}

func TestRuntime_Run_OnErrorYieldIsNotIgnored(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		errs: []error{errors.New("provider boom")},
	}
	hook := &runtimeTestHook{
		onError: func(ctrl *Control) {
			ctrl.Yield("recoverable_error", map[string]any{"stage": "on_error"})
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
		RunID:       "run-on-error-yield",
		AgentID:     "agent-1",
		Instruction: "recover if provider fails",
	})
	if err != nil {
		t.Fatalf("expected on-error yield to override the returned error, got %v", err)
	}
	if result.Status != RunYielded || result.Yield == nil || result.Yield.Reason != "recoverable_error" {
		t.Fatalf("expected yielded result from on-error control, got %#v", result)
	}

	saved := store.runs["run-on-error-yield"]
	if saved == nil || saved.Status != RunYielded {
		t.Fatalf("expected on-error yield to persist yielded state, got %#v", saved)
	}
}
