package agentcore

import (
	"context"
	"errors"
	"testing"
)

func TestRuntime_Run_RejectsResumeForCompletedOrFailed(t *testing.T) {
	tests := []struct {
		name   string
		runID  string
		status RunStatus
	}{
		{name: "completed", runID: "run-complete", status: RunCompleted},
		{name: "failed", runID: "run-failed", status: RunFailed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newRuntimeTestStorage()
			store.runs[tc.runID] = &RunState{
				RunID:   tc.runID,
				AgentID: "agent-1",
				Status:  tc.status,
			}
			store.messages[tc.runID] = []Message{{Role: "assistant", Content: "terminal"}}
			provider := &runtimeTestProvider{
				results: []*GenerateResult{{
					Content:      "should-not-run",
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
				Instruction: "continue",
				ResumeMode:  ResumeFromState,
			})
			if !errors.Is(err, ErrRunNotResumable) {
				t.Fatalf("expected terminal resume to be rejected, got result=%#v err=%v", result, err)
			}
			if len(provider.calls) != 0 {
				t.Fatalf("expected provider not to be called for terminal resume, got %#v", provider.calls)
			}
		})
	}
}

func TestRuntime_Run_RejectsTerminalResumeAcrossResumeModes(t *testing.T) {
	tests := []struct {
		name       string
		status     RunStatus
		resumeMode ResumeMode
	}{
		{name: "completed_auto", status: RunCompleted, resumeMode: ResumeAuto},
		{name: "completed_with_input", status: RunCompleted, resumeMode: ResumeWithInput},
		{name: "failed_auto", status: RunFailed, resumeMode: ResumeAuto},
		{name: "failed_with_input", status: RunFailed, resumeMode: ResumeWithInput},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runID := "run-terminal-" + tc.name
			store := newRuntimeTestStorage()
			store.runs[runID] = &RunState{
				RunID:   runID,
				AgentID: "agent-1",
				Status:  tc.status,
			}
			store.messages[runID] = []Message{{Role: "assistant", Content: "terminal"}}
			provider := &runtimeTestProvider{
				results: []*GenerateResult{{
					Content:      "should-not-run",
					FinishReason: FinishFinal,
				}},
			}

			result, err := New(Config{
				Provider:       provider,
				Storage:        store,
				Tools:          NewToolRegistry(),
				PromptBuilder:  runtimeTestPromptBuilder{},
				ContextBuilder: runtimeTestContextBuilder{},
			}).Run(context.Background(), RunInput{
				RunID:       runID,
				Instruction: "try to continue terminal run",
				ResumeMode:  tc.resumeMode,
			})
			if !errors.Is(err, ErrRunNotResumable) {
				t.Fatalf("expected terminal resume to be rejected, got result=%#v err=%v", result, err)
			}
			if len(provider.calls) != 0 {
				t.Fatalf("expected provider not to be called for terminal resume, got %#v", provider.calls)
			}
			if len(store.messages[runID]) != 1 || store.messages[runID][0].Content != "terminal" {
				t.Fatalf("expected authority transcript to remain unchanged, got %#v", store.messages[runID])
			}
		})
	}
}

func TestRuntime_Run_ResumeMergesInputMetadataIntoStateAndProviderRequest(t *testing.T) {
	runID := "run-resume-metadata-merge"
	store := newRuntimeTestStorage()
	store.runs[runID] = &RunState{
		RunID:   runID,
		AgentID: "agent-1",
		Status:  RunYielded,
		Step:    1,
		Metadata: map[string]any{
			"existing": "keep",
			"override": "old",
		},
	}
	store.messages[runID] = []Message{
		{Role: "user", Content: "persisted question"},
		{Role: "assistant", Content: "persisted draft"},
	}
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done with merged metadata",
			FinishReason: FinishFinal,
		}},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          NewToolRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:      runID,
		ResumeMode: ResumeFromState,
		Metadata: map[string]any{
			"override": "new",
			"added":    "fresh",
		},
	})
	if err != nil {
		t.Fatalf("expected resumed run to succeed, got %v", err)
	}
	if result.Status != RunCompleted || result.Output != "done with merged metadata" {
		t.Fatalf("expected completed result, got %#v", result)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(provider.calls))
	}
	if got := provider.calls[0].Metadata["existing"]; got != "keep" {
		t.Fatalf("expected provider request to keep existing metadata, got %#v", provider.calls[0].Metadata)
	}
	if got := provider.calls[0].Metadata["override"]; got != "new" {
		t.Fatalf("expected provider request to use overridden metadata, got %#v", provider.calls[0].Metadata)
	}
	if got := provider.calls[0].Metadata["added"]; got != "fresh" {
		t.Fatalf("expected provider request to include added metadata, got %#v", provider.calls[0].Metadata)
	}

	saved := store.runs[runID]
	if saved == nil {
		t.Fatal("expected saved state after resume")
	}
	if got := saved.Metadata["existing"]; got != "keep" {
		t.Fatalf("expected saved state to keep existing metadata, got %#v", saved.Metadata)
	}
	if got := saved.Metadata["override"]; got != "new" {
		t.Fatalf("expected saved state to override metadata, got %#v", saved.Metadata)
	}
	if got := saved.Metadata["added"]; got != "fresh" {
		t.Fatalf("expected saved state to include added metadata, got %#v", saved.Metadata)
	}
}

func TestRuntime_Run_TerminalResumeDoesNotMergeInputMetadata(t *testing.T) {
	runID := "run-terminal-resume-metadata"
	store := newRuntimeTestStorage()
	store.runs[runID] = &RunState{
		RunID:   runID,
		AgentID: "agent-1",
		Status:  RunCompleted,
		Metadata: map[string]any{
			"existing": "keep",
		},
	}
	store.messages[runID] = []Message{{Role: "assistant", Content: "terminal"}}
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "should-not-run",
			FinishReason: FinishFinal,
		}},
	}

	_, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          NewToolRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:      runID,
		ResumeMode: ResumeWithInput,
		Metadata: map[string]any{
			"override": "should-not-stick",
		},
		Instruction: "try to continue",
	})
	if !errors.Is(err, ErrRunNotResumable) {
		t.Fatalf("expected terminal resume to be rejected, got %v", err)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("expected provider not to be called, got %#v", provider.calls)
	}
	saved := store.runs[runID]
	if saved == nil {
		t.Fatal("expected saved terminal state to stay present")
	}
	if len(saved.Metadata) != 1 || saved.Metadata["existing"] != "keep" {
		t.Fatalf("expected terminal state metadata to stay unchanged, got %#v", saved.Metadata)
	}
}

func TestRuntime_Run_ResumePreservesMutatedToolResultMetadataAndArtifacts(t *testing.T) {
	runID := "run-resume-mutated-tool-result"
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{
		name: "lookup",
		result: &ToolResult{
			Content: "tool-output",
		},
	})

	yieldHook := &runtimeTestHook{}
	beforeModelCalls := 0
	yieldHook.beforeModel = func(ctrl *Control) {
		beforeModelCalls++
		if beforeModelCalls == 2 {
			ctrl.Yield("await_resume", map[string]any{"step": "after_tool"})
		}
	}
	yieldHook.afterToolEvent = func(event *ToolEvent, _ *Control) {
		event.Result = &ToolResult{
			CallID:  "call-1",
			Name:    "lookup",
			Content: "mutated-output",
			Artifacts: []Artifact{{
				Kind: "file",
				URI:  "artifact://mutated.txt",
			}},
			Metadata: map[string]any{
				"source": "after_tool",
			},
		}
	}

	firstProvider := &runtimeTestProvider{
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

	firstResult, err := New(Config{
		Provider:       firstProvider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{yieldHook},
	}).Run(context.Background(), RunInput{
		RunID:       runID,
		AgentID:     "agent-1",
		Instruction: "collect mutated artifact and resume later",
	})
	if err != nil {
		t.Fatalf("expected first run to yield, got %v", err)
	}
	if firstResult.Status != RunYielded || firstResult.Yield == nil || firstResult.Yield.Reason != "await_resume" {
		t.Fatalf("expected yielded first result, got %#v", firstResult)
	}

	persisted := store.messages[runID]
	if len(persisted) != 3 {
		t.Fatalf("expected yielded transcript with user, assistant tool-call, and tool result, got %#v", persisted)
	}
	if persisted[2].ToolResult == nil || persisted[2].ToolResult.Metadata["source"] != "after_tool" {
		t.Fatalf("expected persisted tool result metadata to survive before resume, got %#v", persisted[2].ToolResult)
	}
	if len(persisted[2].ToolResult.Artifacts) != 1 || persisted[2].ToolResult.Artifacts[0].URI != "artifact://mutated.txt" {
		t.Fatalf("expected persisted tool result artifacts to survive before resume, got %#v", persisted[2].ToolResult)
	}

	resumeProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done after resume with mutated tool result",
			FinishReason: FinishFinal,
		}},
	}

	resumed, err := New(Config{
		Provider:       resumeProvider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:      runID,
		ResumeMode: ResumeFromState,
	})
	if err != nil {
		t.Fatalf("expected resumed run to complete, got %v", err)
	}
	if resumed.Status != RunCompleted || resumed.Output != "done after resume with mutated tool result" {
		t.Fatalf("expected completed resumed result, got %#v", resumed)
	}
	if len(resumeProvider.calls) != 1 || len(resumeProvider.calls[0].Messages) != 3 {
		t.Fatalf("expected resumed provider to receive yielded transcript, got %#v", resumeProvider.calls)
	}
	toolMsg := resumeProvider.calls[0].Messages[2]
	if toolMsg.ToolResult == nil || toolMsg.ToolResult.Content != "mutated-output" {
		t.Fatalf("expected resumed provider to receive mutated tool result, got %#v", toolMsg)
	}
	if toolMsg.ToolResult.Metadata["source"] != "after_tool" {
		t.Fatalf("expected resumed provider to receive mutated tool result metadata, got %#v", toolMsg.ToolResult)
	}
	if len(toolMsg.ToolResult.Artifacts) != 1 || toolMsg.ToolResult.Artifacts[0].URI != "artifact://mutated.txt" {
		t.Fatalf("expected resumed provider to receive mutated tool artifacts, got %#v", toolMsg.ToolResult)
	}

	saved := store.runs[runID]
	if saved == nil || len(saved.Artifacts) != 1 || saved.Artifacts[0].URI != "artifact://mutated.txt" {
		t.Fatalf("expected run state artifacts to remain authoritative across resume, got %#v", saved)
	}
}

func TestRuntime_Run_ResumeLoadsArtifactsFromOptionalArtifactStorage(t *testing.T) {
	runID := "run-resume-artifact-capability"
	store := newRuntimeTestStorage()
	store.runs[runID] = &RunState{
		RunID:   runID,
		AgentID: "agent-1",
		Status:  RunYielded,
		Step:    1,
	}
	store.messages[runID] = []Message{
		{Role: "user", Content: "persisted question"},
		{
			Role:    "tool",
			Content: "tool-output",
			ToolCall: &ToolCall{
				CallID: "call-1",
				Name:   "lookup",
			},
			ToolResult: &ToolResult{
				CallID:  "call-1",
				Name:    "lookup",
				Content: "tool-output",
			},
		},
	}
	store.artifacts[runID] = []Artifact{{
		Kind: "file",
		URI:  "artifact://from-capability.txt",
	}}

	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done",
			FinishReason: FinishFinal,
		}},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          NewToolRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:      runID,
		ResumeMode: ResumeFromState,
	})
	if err != nil {
		t.Fatalf("expected resumed run with artifact capability to complete, got %v", err)
	}
	if result.Status != RunCompleted || result.Output != "done" {
		t.Fatalf("expected completed resumed result, got %#v", result)
	}
	if store.artifactLoadCalls != 1 {
		t.Fatalf("expected runtime to load artifacts from capability on resume, got %d calls", store.artifactLoadCalls)
	}
	saved := store.runs[runID]
	if saved == nil || len(saved.Artifacts) != 1 || saved.Artifacts[0].URI != "artifact://from-capability.txt" {
		t.Fatalf("expected saved state to hydrate artifacts from capability, got %#v", saved)
	}
}

func TestRuntime_Run_ResumeFromOnErrorYieldReusesAuthoritativeTranscript(t *testing.T) {
	store := newRuntimeTestStorage()
	firstProvider := &runtimeTestProvider{
		errs: []error{errors.New("provider boom")},
	}
	hook := &runtimeTestHook{
		onError: func(ctrl *Control) {
			ctrl.Yield("recoverable_error", map[string]any{"stage": "on_error"})
		},
	}

	firstResult, err := New(Config{
		Provider:       firstProvider,
		Storage:        store,
		Tools:          NewToolRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	}).Run(context.Background(), RunInput{
		RunID:       "run-on-error-yield-resume",
		AgentID:     "agent-1",
		Instruction: "recover if provider fails",
	})
	if err != nil {
		t.Fatalf("expected on-error yield to override provider failure, got %v", err)
	}
	if firstResult.Status != RunYielded || firstResult.Yield == nil || firstResult.Yield.Reason != "recoverable_error" {
		t.Fatalf("expected yielded result from on-error control, got %#v", firstResult)
	}

	resumeProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done after recovering from provider failure",
			FinishReason: FinishFinal,
		}},
	}

	resumed, err := New(Config{
		Provider:       resumeProvider,
		Storage:        store,
		Tools:          NewToolRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:      "run-on-error-yield-resume",
		ResumeMode: ResumeFromState,
	})
	if err != nil {
		t.Fatalf("expected resume after on-error yield to complete, got %v", err)
	}
	if resumed.Status != RunCompleted || resumed.Output != "done after recovering from provider failure" {
		t.Fatalf("expected completed resumed result, got %#v", resumed)
	}
	if len(resumeProvider.calls) != 1 || len(resumeProvider.calls[0].Messages) != 1 {
		t.Fatalf("expected resume to reuse only the authoritative user transcript, got %#v", resumeProvider.calls)
	}
	if resumeProvider.calls[0].Messages[0].Role != "user" || resumeProvider.calls[0].Messages[0].Content != "recover if provider fails" {
		t.Fatalf("expected resume to reuse original user input only, got %#v", resumeProvider.calls[0].Messages)
	}

	finalMessages := store.messages["run-on-error-yield-resume"]
	if len(finalMessages) != 2 {
		t.Fatalf("expected resumed authority log to contain user plus final answer, got %#v", finalMessages)
	}
	if finalMessages[0].Role != "user" || finalMessages[1].Role != "assistant" {
		t.Fatalf("unexpected resumed authority transcript shape, got %#v", finalMessages)
	}
}

func TestRuntime_Run_ResumeFromPersistOutputOnErrorYieldDoesNotExposePhantomAssistant(t *testing.T) {
	store := newRuntimeTestStorage()
	store.appendErr = errors.New("append failed")
	store.appendErrOnCall = 2
	firstProvider := &runtimeTestProvider{
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

	firstResult, err := New(Config{
		Provider:       firstProvider,
		Storage:        store,
		Tools:          NewToolRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
		Hooks:          []Hook{hook},
	}).Run(context.Background(), RunInput{
		RunID:       "run-persist-output-yield-resume",
		AgentID:     "agent-1",
		Instruction: "trigger append failure",
	})
	if err != nil {
		t.Fatalf("expected on-error yield to recover persist-output failure, got %v", err)
	}
	if firstResult.Status != RunYielded || firstResult.Yield == nil || firstResult.Yield.Reason != "recover_after_persist_output" {
		t.Fatalf("expected yielded result after persist-output failure, got %#v", firstResult)
	}

	store.appendErr = nil
	store.appendErrOnCall = 0
	resumeProvider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done after retry",
			FinishReason: FinishFinal,
		}},
	}

	resumed, err := New(Config{
		Provider:       resumeProvider,
		Storage:        store,
		Tools:          NewToolRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:      "run-persist-output-yield-resume",
		ResumeMode: ResumeFromState,
	})
	if err != nil {
		t.Fatalf("expected resume after persist-output on-error yield to complete, got %v", err)
	}
	if resumed.Status != RunCompleted || resumed.Output != "done after retry" {
		t.Fatalf("expected completed resumed result, got %#v", resumed)
	}
	if len(resumeProvider.calls) != 1 || len(resumeProvider.calls[0].Messages) != 1 {
		t.Fatalf("expected resume to reuse only authoritative user transcript, got %#v", resumeProvider.calls)
	}
	if resumeProvider.calls[0].Messages[0].Role != "user" || resumeProvider.calls[0].Messages[0].Content != "trigger append failure" {
		t.Fatalf("expected resume to avoid phantom assistant output, got %#v", resumeProvider.calls[0].Messages)
	}

	finalMessages := store.messages["run-persist-output-yield-resume"]
	if len(finalMessages) != 2 {
		t.Fatalf("expected final authority log to contain user plus retried assistant answer, got %#v", finalMessages)
	}
	if finalMessages[1].Role != "assistant" || finalMessages[1].Content != "done after retry" {
		t.Fatalf("expected retried assistant answer to be the only persisted final output, got %#v", finalMessages)
	}
}
