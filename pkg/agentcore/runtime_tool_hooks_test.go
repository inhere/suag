package agentcore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/inhere/suag/pkg/agentcore/tools"
)

func TestRuntime_Run_AfterToolRunsBeforeSuccessfulToolPersist(t *testing.T) {
	store := newRuntimeTestStorage()
	afterToolObserved := false
	store.appendHook = func(_ string, msgs []Message) {
		if len(msgs) == 2 && msgs[0].Role == "assistant" && msgs[1].Role == "tool" && !afterToolObserved {
			t.Fatalf("expected successful tool persist to happen after AfterTool hook")
		}
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
		Tools:          reg,
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
		Tools:          reg,
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
		Tools:          reg,
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
		reg := tools.NewRegistry()
		tool := &runtimeTestTool{name: "lookup"}
		reg.MustRegister(tool)
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
			Tools:          reg,
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
			Tools:          reg,
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
		afterToolErr: errors.New("after tool boom"),
	}

	_, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          reg,
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
		Tools:          reg,
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
	reg := tools.NewRegistry()
	reg.MustRegister(&runtimeTestTool{name: "ok", result: &ToolResult{Content: "fine"}})
	reg.MustRegister(&runtimeTestTool{name: "bad", err: errors.New("boom")})
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
		Provider: provider, Storage: store, Tools: reg,
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
		reg := tools.NewRegistry()
		reg.MustRegister(&runtimeTestTool{name: "lookup"})
		provider := &runtimeTestProvider{results: results}
		res, err := New(Config{
			Provider:       provider,
			Storage:        store,
			Tools:          reg,
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
		if res.WrapUp != "max_steps" {
			t.Fatalf("expected WrapUp max_steps, got %q", res.WrapUp)
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

// wrapUpHook asks for a wrap-up after the model call number at.
type wrapUpHook struct {
	NopHook
	at, calls int
}

func (h *wrapUpHook) AfterModel(_ context.Context, _ *ModelEvent, ctrl *Control) error {
	h.calls++
	if h.calls == h.at {
		ctrl.RequestWrapUp("max_tokens", "TOKENS SPENT, ANSWER NOW")
	}
	return nil
}

func TestRuntime_Run_RequestedWrapUp(t *testing.T) {
	newRun := func(results ...*GenerateResult) (*runtimeTestProvider, *RunResult, error) {
		reg := tools.NewRegistry()
		reg.MustRegister(&runtimeTestTool{name: "lookup"})
		provider := &runtimeTestProvider{results: results}
		res, err := New(Config{
			Provider:       provider,
			Storage:        newRuntimeTestStorage(),
			Tools:          reg,
			PromptBuilder:  runtimeTestPromptBuilder{},
			ContextBuilder: runtimeTestContextBuilder{},
			MaxSteps:       10,
			MaxStepsWrapUp: "WRAP UP",
			Hooks:          []Hook{&wrapUpHook{at: 1}},
		}).Run(context.Background(), RunInput{RunID: "run-req", AgentID: "agent-1", Instruction: "explore"})
		return provider, res, err
	}
	call := &GenerateResult{ToolCalls: []ToolCall{{CallID: "call-1", Name: "lookup"}}, FinishReason: FinishToolCalls}

	t.Run("the next request wraps up and the answer ends the run", func(t *testing.T) {
		provider, res, err := newRun(call, &GenerateResult{Content: "what I found", FinishReason: FinishFinal})
		if err != nil || res == nil || res.Status != RunCompleted || res.Output != "what I found" {
			t.Fatalf("expected completed run, got %#v, %v", res, err)
		}
		if res.WrapUp != "max_tokens" {
			t.Fatalf("expected WrapUp max_tokens, got %q", res.WrapUp)
		}
		last := provider.calls[1].Messages[len(provider.calls[1].Messages)-1]
		if last.Role != "user" || last.Content != "TOKENS SPENT, ANSWER NOW" {
			t.Fatalf("expected the requested instruction last, got %#v", last)
		}
	})
	t.Run("tool calls after the request fail with ErrWrapUpIgnored", func(t *testing.T) {
		provider, _, err := newRun(call, call, call)
		if !errors.Is(err, ErrWrapUpIgnored) || !strings.Contains(err.Error(), "max_tokens") {
			t.Fatalf("expected ErrWrapUpIgnored (max_tokens), got %v", err)
		}
		if len(provider.calls) != 2 {
			t.Fatalf("expected exactly one wrap-up call, got %d calls", len(provider.calls))
		}
	})
	t.Run("a normal answer has no WrapUp", func(t *testing.T) {
		_, res, err := newRun(&GenerateResult{Content: "done", FinishReason: FinishFinal})
		if err != nil || res.WrapUp != "" {
			t.Fatalf("expected a plain answer, got %#v, %v", res, err)
		}
	})
}
