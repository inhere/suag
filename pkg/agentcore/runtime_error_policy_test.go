package agentcore

import (
	"context"
	"errors"
	"github.com/inhere/suag/pkg/agentcore/tools"
	"testing"
)

func TestDefaultErrorPolicy_PreservesExistingSemantics(t *testing.T) {
	policy := DefaultErrorPolicy{}

	decision := policy.HandleRecoverable(context.Background(), &RunState{
		RunID:  "run-1",
		Status: RunRunning,
	}, &ErrorEvent{
		RunID: "run-1",
		Stage: "model",
		Err:   &ProviderError{Err: errors.New("provider boom")},
	})
	if decision.Result != nil || decision.Fail != nil {
		t.Fatalf("expected default error policy not to override recoverable handling, got %#v", decision)
	}
}

func TestRuntime_Run_UsesConfiguredErrorPolicyForRecoverableFailure(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		errs: []error{errors.New("provider boom")},
	}

	policy := &runtimeTestErrorPolicy{
		handleRecoverable: func(_ context.Context, state *RunState, event *ErrorEvent) ErrorDecision {
			if state == nil || state.RunID != "run-error-policy-yield" {
				t.Fatalf("expected policy to receive current run state, got %#v", state)
			}
			if event == nil || event.Stage != "model" {
				t.Fatalf("expected recoverable model error event, got %#v", event)
			}
			var providerErr *ProviderError
			if !errors.As(event.Err, &providerErr) {
				t.Fatalf("expected provider error classification, got %T %v", event.Err, event.Err)
			}
			return ErrorDecision{
				Result: &RunResult{
					Status: RunYielded,
					Yield: &Yield{
						Reason:   "policy_recoverable_error",
						ResumeOK: true,
						Metadata: map[string]any{"stage": event.Stage},
					},
				},
			}
		},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools.NewRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		ErrorPolicy:    policy,
	}).Run(context.Background(), RunInput{
		RunID:       "run-error-policy-yield",
		AgentID:     "agent-1",
		Instruction: "recover provider failure via policy",
	})
	if err != nil {
		t.Fatalf("expected error policy to convert recoverable failure into result, got %v", err)
	}
	if result == nil || result.Status != RunYielded || result.Yield == nil || result.Yield.Reason != "policy_recoverable_error" {
		t.Fatalf("expected yielded result from error policy, got %#v", result)
	}
	saved := store.runs["run-error-policy-yield"]
	if saved == nil || saved.Status != RunYielded {
		t.Fatalf("expected yielded state after error-policy recovery, got %#v", saved)
	}
	if len(store.messages["run-error-policy-yield"]) != 1 || store.messages["run-error-policy-yield"][0].Role != "user" {
		t.Fatalf("expected authority transcript to remain just the user input, got %#v", store.messages["run-error-policy-yield"])
	}
}

func TestRuntime_Run_ErrorPolicyCanBlockHookRecovery(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		errs: []error{errors.New("provider boom")},
	}
	hook := &runtimeTestHook{
		onError: func(ctrl *Control) {
			ctrl.Yield("hook_should_not_win", map[string]any{"source": "hook"})
		},
	}

	policy := &runtimeTestErrorPolicy{
		handleRecoverable: func(_ context.Context, _ *RunState, event *ErrorEvent) ErrorDecision {
			return ErrorDecision{Fail: event.Err}
		},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools.NewRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
		ErrorPolicy:    policy,
	}).Run(context.Background(), RunInput{
		RunID:       "run-error-policy-block-hook",
		AgentID:     "agent-1",
		Instruction: "provider failure should stay terminal",
	})
	if result != nil {
		t.Fatalf("expected error policy fail decision to prevent hook recovery, got %#v", result)
	}
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected provider error to remain terminal, got %T %v", err, err)
	}
	saved := store.runs["run-error-policy-block-hook"]
	if saved == nil || saved.Status != RunFailed {
		t.Fatalf("expected failed state after error policy blocks recovery, got %#v", saved)
	}
}

type runtimeTestErrorPolicy struct {
	handleRecoverable func(context.Context, *RunState, *ErrorEvent) ErrorDecision
}

func (p *runtimeTestErrorPolicy) HandleRecoverable(ctx context.Context, state *RunState, event *ErrorEvent) ErrorDecision {
	if p.handleRecoverable == nil {
		return ErrorDecision{}
	}
	return p.handleRecoverable(ctx, state, event)
}
