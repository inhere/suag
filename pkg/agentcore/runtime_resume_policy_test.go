package agentcore

import (
	"context"
	"errors"
	"github.com/inhere/suag/pkg/agentcore/tools"
	"testing"
)

func TestDefaultResumePolicy_PreservesExistingSemantics(t *testing.T) {
	policy := DefaultResumePolicy{}

	t.Run("allow resume rejects terminal state", func(t *testing.T) {
		err := policy.AllowResume(context.Background(), RunInput{RunID: "run-1"}, &RunState{
			RunID:  "run-1",
			Status: RunCompleted,
		})
		if !errors.Is(err, ErrRunNotResumable) {
			t.Fatalf("expected terminal state to be rejected, got %v", err)
		}
	})

	t.Run("allow resume accepts non terminal state", func(t *testing.T) {
		err := policy.AllowResume(context.Background(), RunInput{RunID: "run-1"}, &RunState{
			RunID:  "run-1",
			Status: RunYielded,
		})
		if err != nil {
			t.Fatalf("expected yielded state to remain resumable, got %v", err)
		}
	})

	t.Run("append instruction keeps current resume matrix", func(t *testing.T) {
		tests := []struct {
			name  string
			input RunInput
			want  bool
		}{
			{
				name:  "from_state never appends",
				input: RunInput{ResumeMode: ResumeFromState, Instruction: "continue"},
				want:  false,
			},
			{
				name:  "with_input appends non empty",
				input: RunInput{ResumeMode: ResumeWithInput, Instruction: "continue"},
				want:  true,
			},
			{
				name:  "with_input appends empty",
				input: RunInput{ResumeMode: ResumeWithInput, Instruction: ""},
				want:  true,
			},
			{
				name:  "auto appends non empty",
				input: RunInput{ResumeMode: ResumeAuto, Instruction: "continue"},
				want:  true,
			},
			{
				name:  "auto skips empty",
				input: RunInput{ResumeMode: ResumeAuto, Instruction: ""},
				want:  false,
			},
			{
				name:  "zero mode follows auto for empty input",
				input: RunInput{ResumeMode: "", Instruction: ""},
				want:  false,
			},
			{
				name:  "zero mode follows auto for non empty input",
				input: RunInput{ResumeMode: "", Instruction: "continue"},
				want:  true,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				got := policy.ShouldAppendInstruction(tc.input, &RunState{
					RunID:  "run-1",
					Status: RunYielded,
				})
				if got != tc.want {
					t.Fatalf("ShouldAppendInstruction() = %v, want %v", got, tc.want)
				}
			})
		}
	})

	t.Run("merge metadata keeps overlay precedence", func(t *testing.T) {
		got := policy.MergeMetadata(
			map[string]any{
				"existing": "keep",
				"override": "old",
			},
			map[string]any{
				"override": "new",
				"added":    "fresh",
			},
		)
		if got["existing"] != "keep" {
			t.Fatalf("expected existing metadata to be preserved, got %#v", got)
		}
		if got["override"] != "new" {
			t.Fatalf("expected overlay metadata to win, got %#v", got)
		}
		if got["added"] != "fresh" {
			t.Fatalf("expected added metadata to be included, got %#v", got)
		}
	})
}

func TestRuntime_Run_UsesConfiguredResumePolicy(t *testing.T) {
	runID := "run-custom-resume-policy"
	store := newRuntimeTestStorage()
	store.runs[runID] = &RunState{
		RunID:   runID,
		AgentID: "agent-1",
		Status:  RunCompleted,
		Metadata: map[string]any{
			"existing": "keep",
		},
	}
	store.messages[runID] = []Message{
		{Role: "user", Content: "persisted"},
		{Role: "assistant", Content: "completed once"},
	}

	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "resumed via policy",
			FinishReason: FinishFinal,
		}},
	}

	policy := &runtimeTestResumePolicy{
		allowResume: func(_ context.Context, input RunInput, state *RunState) error {
			if input.RunID != runID {
				t.Fatalf("expected policy to receive input run id, got %#v", input)
			}
			if state == nil || state.Status != RunCompleted {
				t.Fatalf("expected policy to receive terminal state, got %#v", state)
			}
			return nil
		},
		shouldAppendInstruction: func(input RunInput, state *RunState) bool {
			if input.Instruction != "ignore new input" {
				t.Fatalf("expected policy to receive instruction, got %#v", input)
			}
			if state == nil || state.Status != RunCompleted {
				t.Fatalf("expected policy to receive loaded state, got %#v", state)
			}
			return false
		},
		mergeMetadata: func(stateMetadata, inputMetadata map[string]any) map[string]any {
			merged := cloneMetadata(stateMetadata)
			merged["policy"] = "override"
			merged["request"] = inputMetadata["request"]
			return merged
		},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools.NewRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{prompt: "system"},
		ContextBuilder: runtimeTestContextBuilder{},
		ResumePolicy:   policy,
	}).Run(context.Background(), RunInput{
		RunID:       runID,
		Instruction: "ignore new input",
		ResumeMode:  ResumeWithInput,
		Metadata: map[string]any{
			"request": "fresh",
		},
	})
	if err != nil {
		t.Fatalf("expected custom resume policy to allow resume, got %v", err)
	}
	if result == nil || result.Status != RunCompleted || result.Output != "resumed via policy" {
		t.Fatalf("expected resumed final result, got %#v", result)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected one provider call, got %d", len(provider.calls))
	}
	if len(provider.calls[0].Messages) != 2 {
		t.Fatalf("expected policy to suppress instruction append, got %#v", provider.calls[0].Messages)
	}
	if got := provider.calls[0].Metadata["policy"]; got != "override" {
		t.Fatalf("expected provider request metadata from policy, got %#v", provider.calls[0].Metadata)
	}
	if got := provider.calls[0].Metadata["request"]; got != "fresh" {
		t.Fatalf("expected provider request metadata to include input, got %#v", provider.calls[0].Metadata)
	}

	saved := store.runs[runID]
	if saved == nil {
		t.Fatal("expected resumed run state to be saved")
	}
	if got := saved.Metadata["policy"]; got != "override" {
		t.Fatalf("expected saved state metadata from policy, got %#v", saved.Metadata)
	}
	if len(store.messages[runID]) != 3 || store.messages[runID][2].Content != "resumed via policy" {
		t.Fatalf("expected only final assistant answer to be appended, got %#v", store.messages[runID])
	}
}

type runtimeTestResumePolicy struct {
	allowResume             func(context.Context, RunInput, *RunState) error
	shouldAppendInstruction func(RunInput, *RunState) bool
	mergeMetadata           func(map[string]any, map[string]any) map[string]any
}

func (p *runtimeTestResumePolicy) AllowResume(ctx context.Context, input RunInput, state *RunState) error {
	if p.allowResume == nil {
		return nil
	}
	return p.allowResume(ctx, input, state)
}

func (p *runtimeTestResumePolicy) ShouldAppendInstruction(input RunInput, state *RunState) bool {
	if p.shouldAppendInstruction == nil {
		return false
	}
	return p.shouldAppendInstruction(input, state)
}

func (p *runtimeTestResumePolicy) MergeMetadata(stateMetadata, inputMetadata map[string]any) map[string]any {
	if p.mergeMetadata == nil {
		return nil
	}
	return p.mergeMetadata(stateMetadata, inputMetadata)
}
