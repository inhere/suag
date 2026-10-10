package agentcore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/inhere/suag/pkg/agentcore/tools"
)

func TestDefaultToolExecutionPolicy_PreservesExistingSemantics(t *testing.T) {
	policy := DefaultToolExecutionPolicy{MaxCallsPerStep: 1}

	t.Run("validate tool calls enforces max calls per step", func(t *testing.T) {
		err := policy.ValidateToolCalls(context.Background(), &RunState{
			RunID:  "run-1",
			Status: RunRunning,
		}, []ToolCall{
			{CallID: "call-1", Name: "lookup"},
			{CallID: "call-2", Name: "lookup"},
		})
		if err == nil || !strings.Contains(err.Error(), "max tool calls per step exceeded") {
			t.Fatalf("expected max-tool-calls validation error, got %v", err)
		}
	})

	t.Run("tool error becomes tool message decision", func(t *testing.T) {
		decision := policy.HandleToolError(context.Background(), &RunState{
			RunID:  "run-1",
			Status: RunRunning,
		}, ToolCall{
			CallID: "call-1",
			Name:   "lookup",
		}, errors.New("lookup exploded"))
		if decision.Fail != nil {
			t.Fatalf("expected default policy not to fail run on tool error, got %#v", decision)
		}
		if decision.Result == nil || !strings.Contains(decision.Result.Content, "lookup exploded") {
			t.Fatalf("expected default policy to return tool-result transcript decision, got %#v", decision)
		}
		if decision.Result.CallID != "call-1" || decision.Result.Name != "lookup" {
			t.Fatalf("expected decision result to preserve tool identity, got %#v", decision.Result)
		}
	})
}

func TestRuntime_Run_UsesConfiguredToolExecutionPolicyForValidation(t *testing.T) {
	store := newRuntimeTestStorage()
	reg := tools.NewRegistry()
	tool1 := &runtimeTestTool{name: "lookup-1", result: &ToolResult{Content: "first"}}
	tool2 := &runtimeTestTool{name: "lookup-2", result: &ToolResult{Content: "second"}}
	reg.MustRegister(tool1)
	reg.MustRegister(tool2)

	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{
				{CallID: "call-1", Name: "lookup-1"},
				{CallID: "call-2", Name: "lookup-2"},
			},
			FinishReason: FinishToolCalls,
		}, {
			Content:      "done",
			FinishReason: FinishFinal,
		}},
	}

	policy := &runtimeTestToolExecutionPolicy{
		validateToolCalls: func(_ context.Context, state *RunState, calls []ToolCall) error {
			if state == nil || state.RunID != "run-custom-tool-policy-validate" {
				t.Fatalf("expected policy to receive run state, got %#v", state)
			}
			if len(calls) != 2 {
				t.Fatalf("expected policy to receive both tool calls, got %#v", calls)
			}
			return nil
		},
		handleToolError: func(_ context.Context, _ *RunState, call ToolCall, err error) ToolErrorDecision {
			return ToolErrorDecision{Result: toolErrorAsResult(call, err)}
		},
	}

	result, err := New(Config{
		Provider:            provider,
		Storage:             store,
		Tools:               reg,
		PromptBuilder:       runtimeTestPromptBuilder{},
		ContextBuilder:      runtimeTestContextBuilder{},
		MaxToolCallsPerStep: 1,
		ToolExecutionPolicy: policy,
	}).Run(context.Background(), RunInput{
		RunID:       "run-custom-tool-policy-validate",
		AgentID:     "agent-1",
		Instruction: "allow two tool calls",
	})
	if err != nil {
		t.Fatalf("expected custom tool validation policy to allow batch, got %v", err)
	}
	if result == nil || result.Status != RunCompleted || result.Output != "done" {
		t.Fatalf("expected completed result after both tool calls, got %#v", result)
	}
	if len(tool1.calls) != 1 || len(tool2.calls) != 1 {
		t.Fatalf("expected both tools to execute once, got tool1=%#v tool2=%#v", tool1.calls, tool2.calls)
	}
	if len(provider.calls) != 2 {
		t.Fatalf("expected second model call after tool execution, got %#v", provider.calls)
	}
}

func TestRuntime_Run_UsesConfiguredToolExecutionPolicyForToolErrors(t *testing.T) {
	store := newRuntimeTestStorage()
	reg := tools.NewRegistry()
	reg.MustRegister(&runtimeTestTool{
		name: "lookup",
		err:  errors.New("lookup exploded"),
	})
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls:    []ToolCall{{CallID: "call-1", Name: "lookup"}},
			FinishReason: FinishToolCalls,
		}},
	}

	policy := &runtimeTestToolExecutionPolicy{
		validateToolCalls: func(_ context.Context, _ *RunState, calls []ToolCall) error {
			if len(calls) != 1 || calls[0].Name != "lookup" {
				t.Fatalf("expected validation to see scripted call, got %#v", calls)
			}
			return nil
		},
		handleToolError: func(_ context.Context, state *RunState, call ToolCall, err error) ToolErrorDecision {
			if state == nil || state.RunID != "run-custom-tool-policy-error" {
				t.Fatalf("expected policy to receive current run state, got %#v", state)
			}
			if call.Name != "lookup" || err == nil || !strings.Contains(err.Error(), "lookup exploded") {
				t.Fatalf("expected policy to receive original tool failure, got call=%#v err=%v", call, err)
			}
			return ToolErrorDecision{
				Fail: &ToolError{Err: errors.New("tool policy escalated failure")},
			}
		},
	}

	result, err := New(Config{
		Provider:            provider,
		Storage:             store,
		Tools:               reg,
		PromptBuilder:       runtimeTestPromptBuilder{},
		ContextBuilder:      runtimeTestContextBuilder{},
		ToolExecutionPolicy: policy,
	}).Run(context.Background(), RunInput{
		RunID:       "run-custom-tool-policy-error",
		AgentID:     "agent-1",
		Instruction: "fail on tool error",
	})
	if result != nil {
		t.Fatalf("expected tool policy failure to stop run, got %#v", result)
	}
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected tool policy failure to surface as ToolError, got %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "tool policy escalated failure") {
		t.Fatalf("expected custom policy failure to be preserved, got %v", err)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected provider not to continue after policy failure, got %#v", provider.calls)
	}
	if len(store.messages["run-custom-tool-policy-error"]) != 1 || store.messages["run-custom-tool-policy-error"][0].Role != "user" {
		t.Fatalf("expected failing tool policy not to append tool transcript, got %#v", store.messages["run-custom-tool-policy-error"])
	}
	saved := store.runs["run-custom-tool-policy-error"]
	if saved == nil || saved.Status != RunFailed || !strings.Contains(saved.LastError, "tool policy escalated failure") {
		t.Fatalf("expected failed run state from custom tool policy, got %#v", saved)
	}
}

type runtimeTestToolExecutionPolicy struct {
	validateToolCalls func(context.Context, *RunState, []ToolCall) error
	handleToolError   func(context.Context, *RunState, ToolCall, error) ToolErrorDecision
}

func (p *runtimeTestToolExecutionPolicy) ValidateToolCalls(ctx context.Context, state *RunState, calls []ToolCall) error {
	if p.validateToolCalls == nil {
		return nil
	}
	return p.validateToolCalls(ctx, state, calls)
}

func (p *runtimeTestToolExecutionPolicy) HandleToolError(ctx context.Context, state *RunState, call ToolCall, err error) ToolErrorDecision {
	if p.handleToolError == nil {
		return ToolErrorDecision{}
	}
	return p.handleToolError(ctx, state, call, err)
}
