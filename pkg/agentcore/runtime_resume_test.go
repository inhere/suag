package agentcore

import (
	"context"
	"testing"

	"github.com/inhere/suag/pkg/agentcore/storage/files"
)

func TestRuntime_Run_ResumeMatrix(t *testing.T) {
	tests := []struct {
		name                 string
		runID                string
		resumeMode           ResumeMode
		instruction          string
		wantProviderMessages int
		wantLogMessages      int
		wantLastRole         string
		wantLastContent      string
	}{
		{
			name:                 "auto without instruction does not append user message",
			runID:                "run-auto-no-input",
			resumeMode:           ResumeAuto,
			instruction:          "",
			wantProviderMessages: 2,
			wantLogMessages:      3,
			wantLastRole:         "assistant",
			wantLastContent:      "done",
		},
		{
			name:                 "auto with instruction appends user message",
			runID:                "run-auto-with-input",
			resumeMode:           ResumeAuto,
			instruction:          "continue",
			wantProviderMessages: 3,
			wantLogMessages:      4,
			wantLastRole:         "assistant",
			wantLastContent:      "done",
		},
		{
			name:                 "from state does not append instruction",
			runID:                "run-from-state",
			resumeMode:           ResumeFromState,
			instruction:          "ignore me",
			wantProviderMessages: 2,
			wantLogMessages:      3,
			wantLastRole:         "assistant",
			wantLastContent:      "done",
		},
		{
			name:                 "with input appends user message",
			runID:                "run-with-input",
			resumeMode:           ResumeWithInput,
			instruction:          "continue",
			wantProviderMessages: 3,
			wantLogMessages:      4,
			wantLastRole:         "assistant",
			wantLastContent:      "done",
		},
		{
			name:                 "with input appends even empty instruction",
			runID:                "run-with-empty-input",
			resumeMode:           ResumeWithInput,
			instruction:          "",
			wantProviderMessages: 3,
			wantLogMessages:      4,
			wantLastRole:         "assistant",
			wantLastContent:      "done",
		},
		{
			name:                 "with input preserves whitespace only instruction",
			runID:                "run-with-whitespace-input",
			resumeMode:           ResumeWithInput,
			instruction:          "   ",
			wantProviderMessages: 3,
			wantLogMessages:      4,
			wantLastRole:         "assistant",
			wantLastContent:      "done",
		},
		{
			name:                 "zero resume mode defaults to auto semantics without instruction",
			runID:                "run-zero-mode-no-input",
			resumeMode:           "",
			instruction:          "",
			wantProviderMessages: 2,
			wantLogMessages:      3,
			wantLastRole:         "assistant",
			wantLastContent:      "done",
		},
		{
			name:                 "zero resume mode defaults to auto semantics with instruction",
			runID:                "run-zero-mode-with-input",
			resumeMode:           "",
			instruction:          "continue",
			wantProviderMessages: 3,
			wantLogMessages:      4,
			wantLastRole:         "assistant",
			wantLastContent:      "done",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newRuntimeTestStorage()
			store.runs[tc.runID] = &RunState{
				RunID:   tc.runID,
				AgentID: "agent-1",
				Status:  RunRunning,
				Step:    1,
			}
			store.messages[tc.runID] = []Message{
				{Role: "user", Content: "first"},
				{Role: "assistant", Content: "second"},
			}
			provider := &runtimeTestProvider{
				results: []*GenerateResult{{
					Content:      "done",
					FinishReason: FinishFinal,
				}},
			}

			rt := New(Config{
				Provider:       provider,
				Storage:        store,
				Tools:          NewToolRegistry(),
				PromptBuilder:  runtimeTestPromptBuilder{},
				ContextBuilder: runtimeTestContextBuilder{},
			})

			result, err := rt.Run(context.Background(), RunInput{
				RunID:       tc.runID,
				Instruction: tc.instruction,
				ResumeMode:  tc.resumeMode,
			})
			if err != nil {
				t.Fatalf("expected resume to succeed, got %v", err)
			}
			if result == nil || result.Steps != 2 {
				t.Fatalf("expected resumed run to increment steps, got %#v", result)
			}
			if len(provider.calls) != 1 || len(provider.calls[0].Messages) != tc.wantProviderMessages {
				t.Fatalf("expected %d provider messages, got %#v", tc.wantProviderMessages, provider.calls)
			}
			if provider.calls[0].Messages[0].Content != "first" || provider.calls[0].Messages[1].Content != "second" {
				t.Fatalf("expected persisted message order to be preserved, got %#v", provider.calls[0].Messages)
			}
			if tc.wantProviderMessages == 3 {
				last := provider.calls[0].Messages[2]
				if last.Role != "user" || last.Content != tc.instruction {
					t.Fatalf("expected appended user message to reach provider, got %#v", provider.calls[0].Messages)
				}
			}
			if len(store.messages[tc.runID]) != tc.wantLogMessages {
				t.Fatalf("expected %d persisted messages, got %#v", tc.wantLogMessages, store.messages[tc.runID])
			}
			last := store.messages[tc.runID][len(store.messages[tc.runID])-1]
			if last.Role != tc.wantLastRole || last.Content != tc.wantLastContent {
				t.Fatalf("unexpected final persisted message, got %#v", last)
			}
		})
	}
}

func TestRuntime_Run_ResumeFromState_WithPublicContracts(t *testing.T) {
	runID := "resume-with-public-contracts"
	store := files.NewStorage(t.TempDir())
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID:    "call-echo",
				Name:      "echo",
				Arguments: map[string]any{"text": "persisted"},
			}},
			FinishReason: FinishToolCalls,
		}},
	}
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{
		name: "echo",
		result: &ToolResult{
			Content: "tool:persisted",
		},
	})

	yieldAfterSecondModel := &runtimeTestHook{}
	beforeModelCalls := 0
	yieldAfterSecondModel.beforeModel = func(ctrl *Control) {
		beforeModelCalls++
		if beforeModelCalls == 2 {
			ctrl.Yield("await_resume", map[string]any{"step": "after_tool"})
		}
	}

	firstRuntime := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{yieldAfterSecondModel},
	})

	result, err := firstRuntime.Run(context.Background(), RunInput{
		RunID:       runID,
		AgentID:     "agent-1",
		Instruction: "use a tool and pause",
	})
	if err != nil {
		t.Fatalf("expected first run to yield cleanly, got %v", err)
	}
	if result.Status != RunYielded || result.Yield == nil || result.Yield.Reason != "await_resume" {
		t.Fatalf("expected yielded result before second model call, got %#v", result)
	}

	state, err := store.LoadRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("expected persisted run state, got %v", err)
	}
	if state.Status != RunYielded || state.Step != 1 {
		t.Fatalf("expected yielded state after one model step, got %#v", state)
	}

	messages, err := store.LoadMessages(context.Background(), runID)
	if err != nil {
		t.Fatalf("expected persisted messages, got %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("expected user, assistant tool-call, and tool messages after pause, got %#v", messages)
	}
	if messages[1].Role != "assistant" || messages[1].ToolCall == nil || messages[1].ToolCall.CallID != "call-echo" {
		t.Fatalf("expected persisted assistant tool-call message from adapter chain, got %#v", messages[1])
	}
	if messages[2].Role != "tool" || messages[2].ToolResult == nil || messages[2].ToolResult.Content != "tool:persisted" {
		t.Fatalf("expected persisted tool result content, got %#v", messages[2])
	}

	resumeProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "final after resume",
			FinishReason: FinishFinal,
		}},
	}
	resumeRuntime := New(Config{
		Provider:       resumeProvider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
	})

	result, err = resumeRuntime.Run(context.Background(), RunInput{
		RunID:      runID,
		ResumeMode: ResumeFromState,
	})
	if err != nil {
		t.Fatalf("expected resume to complete, got %v", err)
	}
	if result.Status != RunCompleted || result.Output != "final after resume" {
		t.Fatalf("expected completed resumed result, got %#v", result)
	}
	if result.Steps != 2 {
		t.Fatalf("expected resumed run to continue from step 1 to 2, got %#v", result)
	}

	providerCalls := resumeProvider.calls
	if len(providerCalls) != 1 {
		t.Fatalf("expected one resumed provider call, got %d", len(providerCalls))
	}
	if len(providerCalls[0].Messages) != 3 {
		t.Fatalf("expected resumed provider to receive prior user, assistant tool-call, and tool messages, got %#v", providerCalls[0].Messages)
	}
	if providerCalls[0].Messages[1].Role != "assistant" || providerCalls[0].Messages[1].ToolCall == nil || providerCalls[0].Messages[1].ToolCall.CallID != "call-echo" {
		t.Fatalf("expected resumed provider to receive assistant tool-call message, got %#v", providerCalls[0].Messages[1])
	}
	if providerCalls[0].Messages[2].ToolResult == nil || providerCalls[0].Messages[2].ToolResult.Content != "tool:persisted" {
		t.Fatalf("expected resumed provider to receive persisted tool result, got %#v", providerCalls[0].Messages[2])
	}
	if len(providerCalls[0].Tools) != 1 || providerCalls[0].Tools[0].Name != "echo" {
		t.Fatalf("expected resumed provider to receive public tool schema, got %#v", providerCalls[0].Tools)
	}

	finalState, err := store.LoadRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("expected final persisted state, got %v", err)
	}
	if finalState.Status != RunCompleted || finalState.Step != 2 {
		t.Fatalf("expected completed final state, got %#v", finalState)
	}
	finalMessages, err := store.LoadMessages(context.Background(), runID)
	if err != nil {
		t.Fatalf("expected final persisted messages, got %v", err)
	}
	if len(finalMessages) != 4 || finalMessages[3].Role != "assistant" || finalMessages[3].Content != "final after resume" {
		t.Fatalf("expected assistant output appended after resume, got %#v", finalMessages)
	}
}

func TestRuntime_Run_ResumeAutoAfterYieldWithoutInstruction_ReusesState(t *testing.T) {
	runID := "resume-auto-after-yield"
	store := newRuntimeTestStorage()
	hook := &runtimeTestHook{}
	beforeModelCalls := 0
	hook.beforeModel = func(ctrl *Control) {
		beforeModelCalls++
		if beforeModelCalls == 2 {
			ctrl.Yield("await_resume", map[string]any{"step": "after_tool"})
		}
	}

	firstProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID:    "call-1",
				Name:      "echo",
				Arguments: map[string]any{"msg": "ping"},
			}},
			FinishReason: FinishToolCalls,
		}},
	}
	registry := NewToolRegistry()
	registry.Register(&runtimeTestTool{
		name:   "echo",
		result: &ToolResult{Content: "tool:ping"},
	})

	firstResult, err := New(Config{
		Provider:       firstProvider,
		Storage:        store,
		Tools:          registry,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	}).Run(context.Background(), RunInput{
		RunID:       runID,
		AgentID:     "agent-1",
		Instruction: "resume me later",
	})
	if err != nil {
		t.Fatalf("expected first run to yield, got %v", err)
	}
	if firstResult.Status != RunYielded || firstResult.Yield == nil || firstResult.Yield.Reason != "await_resume" {
		t.Fatalf("expected yielded first result, got %#v", firstResult)
	}

	resumeProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done after auto resume",
			FinishReason: FinishFinal,
		}},
	}
	resumed, err := New(Config{
		Provider:       resumeProvider,
		Storage:        store,
		Tools:          registry,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:      runID,
		ResumeMode: ResumeAuto,
	})
	if err != nil {
		t.Fatalf("expected auto resume to complete, got %v", err)
	}
	if resumed.Status != RunCompleted || resumed.Output != "done after auto resume" {
		t.Fatalf("expected completed resumed result, got %#v", resumed)
	}

	if len(resumeProvider.calls) != 1 {
		t.Fatalf("resume provider calls = %d, want 1", len(resumeProvider.calls))
	}
	if len(resumeProvider.calls[0].Messages) != 3 {
		t.Fatalf("expected auto resume to reuse prior transcript only, got %#v", resumeProvider.calls[0].Messages)
	}
	if resumeProvider.calls[0].Messages[0].Role != "user" || resumeProvider.calls[0].Messages[0].Content != "resume me later" {
		t.Fatalf("expected original user instruction to remain authoritative, got %#v", resumeProvider.calls[0].Messages[0])
	}

	finalMessages := store.messages[runID]
	if len(finalMessages) != 4 {
		t.Fatalf("expected yielded transcript plus final assistant output, got %#v", finalMessages)
	}
	userCount := 0
	for _, msg := range finalMessages {
		if msg.Role == "user" && msg.Content == "resume me later" {
			userCount++
		}
	}
	if userCount != 1 {
		t.Fatalf("expected auto resume not to append a duplicate user message, got %#v", finalMessages)
	}
}

func TestRuntime_Run_ResumeFromStateCanYieldAgainWithoutForkingTranscript(t *testing.T) {
	runID := "resume-from-state-yield-again"
	store := newRuntimeTestStorage()

	firstHook := &runtimeTestHook{}
	firstBeforeModelCalls := 0
	firstHook.beforeModel = func(ctrl *Control) {
		firstBeforeModelCalls++
		if firstBeforeModelCalls == 2 {
			ctrl.Yield("await_resume", map[string]any{"step": "after_tool"})
		}
	}

	registry := NewToolRegistry()
	registry.Register(&runtimeTestTool{
		name:   "echo",
		result: &ToolResult{Content: "tool:ping"},
	})

	firstProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID:    "call-1",
				Name:      "echo",
				Arguments: map[string]any{"msg": "ping"},
			}},
			FinishReason: FinishToolCalls,
		}},
	}

	firstResult, err := New(Config{
		Provider:       firstProvider,
		Storage:        store,
		Tools:          registry,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{firstHook},
	}).Run(context.Background(), RunInput{
		RunID:       runID,
		AgentID:     "agent-1",
		Instruction: "resume me twice",
	})
	if err != nil {
		t.Fatalf("expected first run to yield, got %v", err)
	}
	if firstResult.Status != RunYielded || firstResult.Yield == nil || firstResult.Yield.Reason != "await_resume" {
		t.Fatalf("expected yielded first result, got %#v", firstResult)
	}

	secondHook := &runtimeTestHook{}
	secondHook.beforeModel = func(ctrl *Control) {
		ctrl.Yield("await_resume_again", map[string]any{"step": "before_model"})
	}
	secondProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "should-not-run",
			FinishReason: FinishFinal,
		}},
	}

	secondResult, err := New(Config{
		Provider:       secondProvider,
		Storage:        store,
		Tools:          registry,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{secondHook},
	}).Run(context.Background(), RunInput{
		RunID:      runID,
		ResumeMode: ResumeFromState,
	})
	if err != nil {
		t.Fatalf("expected second resume to yield again, got %v", err)
	}
	if secondResult.Status != RunYielded || secondResult.Yield == nil || secondResult.Yield.Reason != "await_resume_again" {
		t.Fatalf("expected second yielded result, got %#v", secondResult)
	}
	if len(secondProvider.calls) != 0 {
		t.Fatalf("expected provider not to run on second yield, got %#v", secondProvider.calls)
	}

	resumeProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done after repeated resume",
			FinishReason: FinishFinal,
		}},
	}
	resumed, err := New(Config{
		Provider:       resumeProvider,
		Storage:        store,
		Tools:          registry,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:      runID,
		ResumeMode: ResumeFromState,
	})
	if err != nil {
		t.Fatalf("expected final resume to complete, got %v", err)
	}
	if resumed.Status != RunCompleted || resumed.Output != "done after repeated resume" {
		t.Fatalf("expected completed result after repeated resume, got %#v", resumed)
	}
	if len(resumeProvider.calls) != 1 || len(resumeProvider.calls[0].Messages) != 3 {
		t.Fatalf("expected final resume to reuse yielded transcript only, got %#v", resumeProvider.calls)
	}

	finalMessages := store.messages[runID]
	if len(finalMessages) != 4 {
		t.Fatalf("expected yielded transcript plus final answer without duplicates, got %#v", finalMessages)
	}
	if finalMessages[0].Role != "user" || finalMessages[1].Role != "assistant" || finalMessages[2].Role != "tool" || finalMessages[3].Role != "assistant" {
		t.Fatalf("unexpected repeated-resume transcript shape, got %#v", finalMessages)
	}
}

func TestRuntime_Run_ResumeWithInputThenYieldAgainKeepsAppendedInputAuthoritative(t *testing.T) {
	runID := "resume-with-input-yield-again"
	store := newRuntimeTestStorage()

	firstHook := &runtimeTestHook{}
	firstBeforeModelCalls := 0
	firstHook.beforeModel = func(ctrl *Control) {
		firstBeforeModelCalls++
		if firstBeforeModelCalls == 2 {
			ctrl.Yield("await_human", map[string]any{"step": "after_tool"})
		}
	}

	registry := NewToolRegistry()
	registry.Register(&runtimeTestTool{
		name:   "echo",
		result: &ToolResult{Content: "tool:ping"},
	})

	firstProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID:    "call-1",
				Name:      "echo",
				Arguments: map[string]any{"msg": "ping"},
			}},
			FinishReason: FinishToolCalls,
		}},
	}

	_, err := New(Config{
		Provider:       firstProvider,
		Storage:        store,
		Tools:          registry,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{firstHook},
	}).Run(context.Background(), RunInput{
		RunID:       runID,
		AgentID:     "agent-1",
		Instruction: "wait for my follow-up",
	})
	if err != nil {
		t.Fatalf("expected first run to yield, got %v", err)
	}

	secondHook := &runtimeTestHook{}
	secondHook.beforeModel = func(ctrl *Control) {
		ctrl.Yield("await_confirmation", map[string]any{"step": "before_model"})
	}
	secondProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "should-not-run",
			FinishReason: FinishFinal,
		}},
	}

	secondResult, err := New(Config{
		Provider:       secondProvider,
		Storage:        store,
		Tools:          registry,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{secondHook},
	}).Run(context.Background(), RunInput{
		RunID:       runID,
		Instruction: "continue with my approved input",
		ResumeMode:  ResumeWithInput,
	})
	if err != nil {
		t.Fatalf("expected second run to yield again, got %v", err)
	}
	if secondResult.Status != RunYielded || secondResult.Yield == nil || secondResult.Yield.Reason != "await_confirmation" {
		t.Fatalf("expected second yielded result, got %#v", secondResult)
	}
	if len(secondProvider.calls) != 0 {
		t.Fatalf("expected provider not to run before second yield, got %#v", secondProvider.calls)
	}

	resumeProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done after human follow-up",
			FinishReason: FinishFinal,
		}},
	}
	resumed, err := New(Config{
		Provider:       resumeProvider,
		Storage:        store,
		Tools:          registry,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:      runID,
		ResumeMode: ResumeFromState,
	})
	if err != nil {
		t.Fatalf("expected final resume to complete, got %v", err)
	}
	if resumed.Status != RunCompleted || resumed.Output != "done after human follow-up" {
		t.Fatalf("expected final completed result, got %#v", resumed)
	}
	if len(resumeProvider.calls) != 1 || len(resumeProvider.calls[0].Messages) != 4 {
		t.Fatalf("expected final resume to include appended human input exactly once, got %#v", resumeProvider.calls)
	}
	lastInput := resumeProvider.calls[0].Messages[3]
	if lastInput.Role != "user" || lastInput.Content != "continue with my approved input" {
		t.Fatalf("expected appended human input to remain authoritative before final answer, got %#v", resumeProvider.calls[0].Messages)
	}

	finalMessages := store.messages[runID]
	if len(finalMessages) != 5 {
		t.Fatalf("expected transcript to keep tool transcript, appended user input, and final answer, got %#v", finalMessages)
	}
	if finalMessages[3].Role != "user" || finalMessages[3].Content != "continue with my approved input" {
		t.Fatalf("expected appended user input to remain in authority transcript, got %#v", finalMessages)
	}
	if finalMessages[4].Role != "assistant" || finalMessages[4].Content != "done after human follow-up" {
		t.Fatalf("expected final assistant answer after appended user input, got %#v", finalMessages)
	}
}
