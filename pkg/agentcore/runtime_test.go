package agentcore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type testStorage struct{}

func (testStorage) LoadRun(context.Context, string) (*RunState, error)      { return nil, nil }
func (testStorage) LoadMessages(context.Context, string) ([]Message, error) { return nil, nil }
func (testStorage) SaveRun(context.Context, *RunState) error                { return nil }
func (testStorage) AppendMessages(context.Context, string, ...Message) error {
	return nil
}

type testPromptBuilder struct{}

func (testPromptBuilder) BuildSystemPrompt(context.Context, *RunState) (string, error) {
	return "", nil
}

type testContextBuilder struct{}

func (testContextBuilder) BuildMessages(context.Context, *RunState) ([]Message, error) {
	return nil, nil
}

func TestGenerateResultValidate_RejectsEmptyResult(t *testing.T) {
	res := &GenerateResult{}
	err := res.Validate()
	if !errors.Is(err, ErrEmptyGenerateResult) {
		t.Fatalf("expected empty result error, got %v", err)
	}
}

func TestGenerateResultValidate_RejectsMixedFinalAndToolCalls(t *testing.T) {
	res := &GenerateResult{
		Content:      "done",
		ToolCalls:    []ToolCall{{CallID: "call-1", Name: "echo"}},
		FinishReason: FinishToolCalls,
	}

	err := res.Validate()
	if !errors.Is(err, ErrMixedGenerateResult) {
		t.Fatalf("expected mutually exclusive error, got %v", err)
	}
}

func TestGenerateResultValidate_RejectsToolCallWithoutID(t *testing.T) {
	res := &GenerateResult{
		ToolCalls:    []ToolCall{{Name: "echo"}},
		FinishReason: FinishToolCalls,
	}

	err := res.Validate()
	if !errors.Is(err, ErrMissingToolCallID) {
		t.Fatalf("expected missing call id error, got %v", err)
	}
}

func TestGenerateResultValidate_RejectsToolCallWithoutName(t *testing.T) {
	res := &GenerateResult{
		ToolCalls:    []ToolCall{{CallID: "call-1"}},
		FinishReason: FinishToolCalls,
	}

	err := res.Validate()
	if !errors.Is(err, ErrMissingToolName) {
		t.Fatalf("expected missing tool name error, got %v", err)
	}
}

func TestGenerateResultValidate_RejectsMissingFinishReason(t *testing.T) {
	res := &GenerateResult{Content: "done"}
	err := res.Validate()
	if !errors.Is(err, ErrMissingFinishReason) {
		t.Fatalf("expected missing finish reason error, got %v", err)
	}
}

func TestGenerateResultValidate_RejectsFinalContentWithToolFinishReason(t *testing.T) {
	res := &GenerateResult{
		Content:      "done",
		FinishReason: FinishToolCalls,
	}

	err := res.Validate()
	if !errors.Is(err, ErrInvalidFinishReason) {
		t.Fatalf("expected invalid finish reason error, got %v", err)
	}
}

func TestGenerateResultValidate_RejectsToolCallsWithFinalFinishReason(t *testing.T) {
	res := &GenerateResult{
		ToolCalls:    []ToolCall{{CallID: "call-1", Name: "echo"}},
		FinishReason: FinishFinal,
	}

	err := res.Validate()
	if !errors.Is(err, ErrInvalidFinishReason) {
		t.Fatalf("expected invalid finish reason error, got %v", err)
	}
}

func TestControl_Yield(t *testing.T) {
	ctrl := NewControl()
	ctrl.Yield("await_approval", map[string]any{"policy": "manual"})
	if !ctrl.ShouldYield() {
		t.Fatal("expected control to yield")
	}
}

func TestControl_YieldCopiesMetadata(t *testing.T) {
	metadata := map[string]any{"policy": "manual"}
	ctrl := NewControl()
	ctrl.Yield("await_approval", metadata)

	metadata["policy"] = "auto"

	if got := ctrl.YieldMetadata["policy"]; got != "manual" {
		t.Fatalf("expected copied metadata to stay manual, got %v", got)
	}
}

func TestControl_ShortCircuit(t *testing.T) {
	ctrl := NewControl()
	ctrl.ShortCircuit("replacement")

	if !ctrl.ShouldShortCircuit() {
		t.Fatal("expected control to short circuit")
	}
	if ctrl.Replacement != "replacement" {
		t.Fatalf("expected replacement to be preserved, got %v", ctrl.Replacement)
	}
}

func TestRuntimeErrorCategories_ArePublicWrappers(t *testing.T) {
	base := errors.New("boom")

	errs := []error{
		&ProviderError{Err: base},
		&ToolError{Err: base},
		&HookError{Err: base},
		&StorageError{Err: base},
	}

	if len(errs) != 4 {
		t.Fatal("expected public runtime error categories to be instantiable")
	}

	var providerErr *ProviderError
	if !errors.As(errs[0], &providerErr) || !errors.Is(errs[0], base) {
		t.Fatalf("expected provider error wrapper semantics, got %v", errs[0])
	}
}

func TestRunStateAndRunResult_ArePublicTypes(t *testing.T) {
	state := RunState{
		RunID:     "run-1",
		AgentID:   "agent-1",
		Status:    RunRunning,
		Step:      2,
		Messages:  []Message{{Role: "user", Content: "hello"}},
		Artifacts: []Artifact{{Kind: "file", URI: "out.txt"}},
		Metadata:  map[string]any{"scope": "contract"},
		Scratch:   map[string]any{"planner": "draft"},
		Usage:     Usage{TotalTokens: 5},
		LastError: "previous failure",
	}
	result := RunResult{
		RunID:  "run-1",
		Status: RunYielded,
		Output: "paused",
		Steps:  2,
		Usage:  Usage{TotalTokens: 5},
		Yield:  &Yield{Reason: "await_approval", ResumeOK: true},
	}

	if state.RunID == "" || result.RunID == "" {
		t.Fatal("expected public run contracts to be instantiable")
	}
}

func TestYield_IncludesResumeOK(t *testing.T) {
	yield := Yield{
		Reason:   "await_approval",
		ResumeOK: true,
	}

	if !yield.ResumeOK {
		t.Fatal("expected yield resume flag to be publicly available")
	}
}

func TestStorageContract_IncludesMessagePersistenceMethods(t *testing.T) {
	var _ Storage = (*testStorage)(nil)
}

func TestPromptAndContextBuilderContracts_UseRunState(t *testing.T) {
	var _ PromptBuilder = (*testPromptBuilder)(nil)
	var _ ContextBuilder = (*testContextBuilder)(nil)
}

type runtimeTestPromptBuilder struct {
	prompt  string
	err     error
	calls   *int
	onBuild func(*RunState) error
}

func (b runtimeTestPromptBuilder) BuildSystemPrompt(_ context.Context, state *RunState) (string, error) {
	if b.calls != nil {
		*b.calls = *b.calls + 1
	}
	if b.onBuild != nil {
		if err := b.onBuild(state); err != nil {
			return "", err
		}
	}
	if b.err != nil {
		return "", b.err
	}
	return b.prompt, nil
}

type runtimeTestContextBuilder struct {
	err     error
	calls   *int
	onBuild func(*RunState) error
}

func (b runtimeTestContextBuilder) BuildMessages(_ context.Context, state *RunState) ([]Message, error) {
	if b.calls != nil {
		*b.calls = *b.calls + 1
	}
	if b.onBuild != nil {
		if err := b.onBuild(state); err != nil {
			return nil, err
		}
	}
	if b.err != nil {
		return nil, b.err
	}
	if state == nil || len(state.Messages) == 0 {
		return nil, nil
	}
	msgs := make([]Message, len(state.Messages))
	copy(msgs, state.Messages)
	return msgs, nil
}

type runtimeTestProvider struct {
	results []*GenerateResult
	errs    []error
	calls   []*GenerateRequest
}

func (p *runtimeTestProvider) Name() string {
	return "runtime-test"
}

func (p *runtimeTestProvider) Generate(_ context.Context, req *GenerateRequest) (*GenerateResult, error) {
	p.calls = append(p.calls, cloneGenerateRequest(req))

	if len(p.errs) > 0 {
		err := p.errs[0]
		p.errs = p.errs[1:]
		return nil, err
	}
	if len(p.results) == 0 {
		return nil, fmt.Errorf("no scripted provider result")
	}

	result := cloneGenerateResult(p.results[0])
	p.results = p.results[1:]
	return result, nil
}

type runtimeTestStorage struct {
	runs            map[string]*RunState
	messages        map[string][]Message
	loadRunErr      error
	loadMsgsErr     error
	saveRunErr      error
	appendErr       error
	appendErrOnCall int
	appendCalls     int
}

func newRuntimeTestStorage() *runtimeTestStorage {
	return &runtimeTestStorage{
		runs:     make(map[string]*RunState),
		messages: make(map[string][]Message),
	}
}

func (s *runtimeTestStorage) LoadRun(_ context.Context, runID string) (*RunState, error) {
	if s.loadRunErr != nil {
		return nil, s.loadRunErr
	}
	return cloneRunState(s.runs[runID]), nil
}

func (s *runtimeTestStorage) LoadMessages(_ context.Context, runID string) ([]Message, error) {
	if s.loadMsgsErr != nil {
		return nil, s.loadMsgsErr
	}
	return cloneMessages(s.messages[runID]), nil
}

func (s *runtimeTestStorage) SaveRun(_ context.Context, state *RunState) error {
	if s.saveRunErr != nil {
		return s.saveRunErr
	}
	if state == nil {
		return nil
	}
	s.runs[state.RunID] = cloneRunState(state)
	return nil
}

func (s *runtimeTestStorage) AppendMessages(_ context.Context, runID string, msgs ...Message) error {
	s.appendCalls++
	if s.appendErrOnCall > 0 && s.appendCalls == s.appendErrOnCall {
		return s.appendErr
	}
	if s.appendErr != nil && s.appendErrOnCall == 0 {
		return s.appendErr
	}
	s.messages[runID] = append(s.messages[runID], cloneMessages(msgs)...)
	return nil
}

type runtimeTestHook struct {
	NopHook
	beforeRun      func(*Control)
	beforePrompt   func(*Control)
	beforeContext  func(*Control)
	beforeModel    func(*Control)
	afterModel     func(*Control)
	afterRun       func(*Control)
	onError        func(*Control)
	beforeModelErr error
	beforeRunCalls int
}

func (h *runtimeTestHook) BeforeRun(_ context.Context, _ *RunEvent, ctrl *Control) error {
	h.beforeRunCalls++
	if h.beforeRun != nil {
		h.beforeRun(ctrl)
	}
	return nil
}

func (h *runtimeTestHook) BeforePromptBuild(_ context.Context, _ *PromptEvent, ctrl *Control) error {
	if h.beforePrompt != nil {
		h.beforePrompt(ctrl)
	}
	return nil
}

func (h *runtimeTestHook) BeforeContextBuild(_ context.Context, _ *ContextEvent, ctrl *Control) error {
	if h.beforeContext != nil {
		h.beforeContext(ctrl)
	}
	return nil
}

func (h *runtimeTestHook) BeforeModel(_ context.Context, _ *ModelEvent, ctrl *Control) error {
	if h.beforeModel != nil {
		h.beforeModel(ctrl)
	}
	return h.beforeModelErr
}

func (h *runtimeTestHook) AfterModel(_ context.Context, _ *ModelEvent, ctrl *Control) error {
	if h.afterModel != nil {
		h.afterModel(ctrl)
	}
	return nil
}

func (h *runtimeTestHook) AfterRun(_ context.Context, _ *RunEvent, ctrl *Control) error {
	if h.afterRun != nil {
		h.afterRun(ctrl)
	}
	return nil
}

func (h *runtimeTestHook) OnError(_ context.Context, _ *ErrorEvent, ctrl *Control) error {
	if h.onError != nil {
		h.onError(ctrl)
	}
	return nil
}

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
		Tools:          NewToolRegistry(),
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
	unique := filepath.Base(t.TempDir()) + "-unsafe"
	outsidePath := filepath.Join(os.TempDir(), unique)
	_ = os.RemoveAll(outsidePath)
	defer os.RemoveAll(outsidePath)

	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "ok",
			FinishReason: FinishFinal,
		}},
	}

	rt := New(Config{Provider: provider})

	_, err := rt.Run(context.Background(), RunInput{
		RunID:       filepath.Join("..", unique),
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
	if _, statErr := os.Stat(outsidePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected unsafe run id not to create %q, got %v", outsidePath, statErr)
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
		Tools:          NewToolRegistry(),
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
				Tools:          NewToolRegistry(),
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

func TestRuntime_Run_ResumeFromState_DoesNotAppendInstruction(t *testing.T) {
	store := newRuntimeTestStorage()
	store.runs["run-resume"] = &RunState{
		RunID:   "run-resume",
		AgentID: "agent-1",
		Status:  RunRunning,
		Step:    1,
	}
	store.messages["run-resume"] = []Message{
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
		RunID:       "run-resume",
		Instruction: "ignore me",
		ResumeMode:  ResumeFromState,
	})
	if err != nil {
		t.Fatalf("expected resume from state to succeed, got %v", err)
	}
	if len(provider.calls) != 1 || len(provider.calls[0].Messages) != 2 {
		t.Fatalf("expected resume from state to reuse only persisted messages, got %#v", provider.calls)
	}
	if provider.calls[0].Messages[1].Content != "second" {
		t.Fatalf("expected persisted message order to be preserved, got %#v", provider.calls[0].Messages)
	}
	if len(store.messages["run-resume"]) != 3 || store.messages["run-resume"][2].Content != "done" {
		t.Fatalf("expected only assistant final answer to be appended on resume, got %#v", store.messages["run-resume"])
	}
	if result.Steps != 2 {
		t.Fatalf("expected resumed run to increment steps, got %#v", result)
	}
}

func TestRuntime_Run_ResumeWithInput_AppendsInstruction(t *testing.T) {
	store := newRuntimeTestStorage()
	store.runs["run-resume-input"] = &RunState{
		RunID:   "run-resume-input",
		AgentID: "agent-1",
		Status:  RunRunning,
		Step:    1,
	}
	store.messages["run-resume-input"] = []Message{
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

	_, err := rt.Run(context.Background(), RunInput{
		RunID:       "run-resume-input",
		Instruction: "continue",
		ResumeMode:  ResumeWithInput,
	})
	if err != nil {
		t.Fatalf("expected resume with input to succeed, got %v", err)
	}
	if len(provider.calls) != 1 || len(provider.calls[0].Messages) != 3 {
		t.Fatalf("expected resume with input to append a new user message, got %#v", provider.calls)
	}
	if provider.calls[0].Messages[2].Role != "user" || provider.calls[0].Messages[2].Content != "continue" {
		t.Fatalf("expected appended user message to reach the provider, got %#v", provider.calls[0].Messages)
	}
	if len(store.messages["run-resume-input"]) != 4 {
		t.Fatalf("expected message log to include continued user input and assistant reply, got %#v", store.messages["run-resume-input"])
	}
}

func TestRuntime_Run_ResumeAuto_AppendsOnlyWhenInstructionExists(t *testing.T) {
	tests := []struct {
		name         string
		instruction  string
		wantMessages int
	}{
		{name: "without instruction", instruction: "", wantMessages: 2},
		{name: "with instruction", instruction: "continue", wantMessages: 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newRuntimeTestStorage()
			store.runs["run-auto"] = &RunState{
				RunID:   "run-auto",
				AgentID: "agent-1",
				Status:  RunRunning,
			}
			store.messages["run-auto"] = []Message{
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

			_, err := rt.Run(context.Background(), RunInput{
				RunID:       "run-auto",
				Instruction: tc.instruction,
				ResumeMode:  ResumeAuto,
			})
			if err != nil {
				t.Fatalf("expected auto resume to succeed, got %v", err)
			}
			if len(provider.calls) != 1 || len(provider.calls[0].Messages) != tc.wantMessages {
				t.Fatalf("expected %d messages for auto resume, got %#v", tc.wantMessages, provider.calls)
			}
		})
	}
}

func TestRuntime_Run_CompletedRunCannotResume(t *testing.T) {
	store := newRuntimeTestStorage()
	store.runs["run-complete"] = &RunState{
		RunID:   "run-complete",
		AgentID: "agent-1",
		Status:  RunCompleted,
	}
	store.messages["run-complete"] = []Message{{Role: "assistant", Content: "done"}}
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
		RunID:      "run-complete",
		ResumeMode: ResumeFromState,
	})
	if !errors.Is(err, ErrRunNotResumable) {
		t.Fatalf("expected completed run resume to be rejected, got result=%#v err=%v", result, err)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("expected provider not to be called for terminal resume, got %#v", provider.calls)
	}
}

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
		Tools:          NewToolRegistry(),
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
		Tools:    NewToolRegistry(),
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
		Tools:         NewToolRegistry(),
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
		Tools:          NewToolRegistry(),
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
		Tools:          NewToolRegistry(),
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
		Tools:          NewToolRegistry(),
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
		Tools:          NewToolRegistry(),
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

func TestRuntime_Run_ToolCallsNotImplemented(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID: "call-1",
				Name:   "echo",
			}},
			FinishReason: FinishToolCalls,
		}},
	}

	_, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          NewToolRegistry(),
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:       "run-tool-calls",
		AgentID:     "agent-1",
		Instruction: "use a tool",
	})
	if !errors.Is(err, ErrToolCallsNotImplemented) {
		t.Fatalf("expected tool-calls-not-implemented error, got %v", err)
	}
	saved := store.runs["run-tool-calls"]
	if saved == nil || saved.Status != RunFailed || saved.LastError == "" {
		t.Fatalf("expected tool-call failure state to be persisted, got %#v", saved)
	}
	if len(store.messages["run-tool-calls"]) != 1 || store.messages["run-tool-calls"][0].Content != "use a tool" {
		t.Fatalf("expected only the user message to be persisted on tool-call failure, got %#v", store.messages["run-tool-calls"])
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
		Tools:          NewToolRegistry(),
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

func TestRuntime_Run_ProviderErrorMapped(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		errs: []error{errors.New("provider boom")},
	}

	rt := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          NewToolRegistry(),
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
		Tools:          NewToolRegistry(),
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
		Tools:          NewToolRegistry(),
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

func cloneGenerateRequest(req *GenerateRequest) *GenerateRequest {
	if req == nil {
		return nil
	}
	cloned := &GenerateRequest{
		Model:       req.Model,
		System:      req.System,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Metadata:    cloneMetadata(req.Metadata),
	}
	if len(req.Messages) > 0 {
		cloned.Messages = cloneMessages(req.Messages)
	}
	if len(req.Tools) > 0 {
		cloned.Tools = make([]ToolSchema, len(req.Tools))
		for i, tool := range req.Tools {
			cloned.Tools[i] = ToolSchema{
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: cloneMetadata(tool.InputSchema),
			}
		}
	}
	return cloned
}

func cloneGenerateResult(result *GenerateResult) *GenerateResult {
	if result == nil {
		return nil
	}
	cloned := &GenerateResult{
		Content:      result.Content,
		FinishReason: result.FinishReason,
		Usage:        result.Usage,
		Artifacts:    append([]Artifact(nil), result.Artifacts...),
		Metadata:     cloneMetadata(result.Metadata),
	}
	if len(result.ToolCalls) > 0 {
		cloned.ToolCalls = make([]ToolCall, len(result.ToolCalls))
		for i, call := range result.ToolCalls {
			cloned.ToolCalls[i] = ToolCall{
				CallID:    call.CallID,
				Name:      call.Name,
				Arguments: cloneMetadata(call.Arguments),
			}
		}
	}
	return cloned
}

func cloneRunState(state *RunState) *RunState {
	if state == nil {
		return nil
	}
	cloned := *state
	cloned.Messages = cloneMessages(state.Messages)
	cloned.Artifacts = append([]Artifact(nil), state.Artifacts...)
	cloned.Metadata = cloneMetadata(state.Metadata)
	cloned.Scratch = cloneMetadata(state.Scratch)
	return &cloned
}

func cloneMessages(messages []Message) []Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]Message, len(messages))
	for i, msg := range messages {
		cloned[i] = Message{
			Role:       msg.Role,
			Content:    msg.Content,
			Metadata:   cloneMetadata(msg.Metadata),
			ToolCall:   cloneToolCall(msg.ToolCall),
			ToolResult: cloneToolResult(msg.ToolResult),
		}
	}
	return cloned
}

func cloneToolCall(call *ToolCall) *ToolCall {
	if call == nil {
		return nil
	}
	cloned := *call
	cloned.Arguments = cloneMetadata(call.Arguments)
	return &cloned
}

func cloneToolResult(result *ToolResult) *ToolResult {
	if result == nil {
		return nil
	}
	cloned := *result
	cloned.Artifacts = append([]Artifact(nil), result.Artifacts...)
	cloned.Metadata = cloneMetadata(result.Metadata)
	return &cloned
}
