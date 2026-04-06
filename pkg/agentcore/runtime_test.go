package agentcore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inhere/axon/internal/llm"
	internaltoolsimpl "github.com/inhere/axon/internal/tools"
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

type runtimeTestTool struct {
	name        string
	description string
	result      *ToolResult
	err         error
	handler     func(ToolCall) (*ToolResult, error)
	calls       []ToolCall
}

func (t *runtimeTestTool) Name() string {
	return t.name
}

func (t *runtimeTestTool) Description() string {
	if t.description != "" {
		return t.description
	}
	return "runtime test tool"
}

func (t *runtimeTestTool) Schema() ToolSchema {
	return ToolSchema{
		Name:        t.name,
		Description: t.Description(),
		InputSchema: map[string]any{"type": "object"},
	}
}

func (t *runtimeTestTool) Execute(_ context.Context, call ToolCall) (*ToolResult, error) {
	t.calls = append(t.calls, call)
	if t.handler != nil {
		return t.handler(call)
	}
	if t.err != nil {
		return nil, t.err
	}
	if t.result == nil {
		return &ToolResult{CallID: call.CallID, Name: call.Name}, nil
	}
	result := cloneToolResult(t.result)
	if result.CallID == "" {
		result.CallID = call.CallID
	}
	if result.Name == "" {
		result.Name = call.Name
	}
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
	appendHook      func(string, []Message)
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
	if s.appendHook != nil {
		s.appendHook(runID, cloneMessages(msgs))
	}
	s.messages[runID] = append(s.messages[runID], cloneMessages(msgs)...)
	return nil
}

type runtimeTestHook struct {
	NopHook
	beforeRun       func(*Control)
	beforePrompt    func(*Control)
	beforeContext   func(*Control)
	beforeModel     func(*Control)
	afterModel      func(*Control)
	beforeTool      func(*Control)
	beforeToolEvent func(*ToolEvent, *Control)
	afterTool       func(*Control)
	afterToolEvent  func(*ToolEvent, *Control)
	afterToolErr    error
	afterRun        func(*Control)
	afterRunEvent   func(*RunEvent, *Control)
	afterRunErr     error
	onError         func(*Control)
	onErrorEvent    func(*ErrorEvent, *Control)
	onErrorReturn   func(*ErrorEvent, *Control) error
	beforeModelErr  error
	beforeRunCalls  int
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

func (h *runtimeTestHook) BeforeTool(_ context.Context, event *ToolEvent, ctrl *Control) error {
	if h.beforeTool != nil {
		h.beforeTool(ctrl)
	}
	if h.beforeToolEvent != nil {
		h.beforeToolEvent(event, ctrl)
	}
	return nil
}

func (h *runtimeTestHook) AfterTool(_ context.Context, event *ToolEvent, ctrl *Control) error {
	if h.afterTool != nil {
		h.afterTool(ctrl)
	}
	if h.afterToolEvent != nil {
		h.afterToolEvent(event, ctrl)
	}
	return h.afterToolErr
}

func (h *runtimeTestHook) AfterRun(_ context.Context, event *RunEvent, ctrl *Control) error {
	if h.afterRun != nil {
		h.afterRun(ctrl)
	}
	if h.afterRunEvent != nil {
		h.afterRunEvent(event, ctrl)
	}
	return h.afterRunErr
}

func (h *runtimeTestHook) OnError(_ context.Context, event *ErrorEvent, ctrl *Control) error {
	if h.onError != nil {
		h.onError(ctrl)
	}
	if h.onErrorEvent != nil {
		h.onErrorEvent(event, ctrl)
	}
	if h.onErrorReturn != nil {
		return h.onErrorReturn(event, ctrl)
	}
	return nil
}

func afterRunErrorHook(err error) *runtimeTestHook {
	return &runtimeTestHook{afterRunErr: err}
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

type runtimeAdapterEchoTool struct{}

func (runtimeAdapterEchoTool) Name() string        { return "echo" }
func (runtimeAdapterEchoTool) Description() string { return "Echo input text" }
func (runtimeAdapterEchoTool) Kind() internaltoolsimpl.Kind {
	return internaltoolsimpl.KindNative
}
func (runtimeAdapterEchoTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        "echo",
		Description: "Echo input text",
		Parameters: llm.SchemaProperty{
			Type: "object",
			Properties: map[string]*llm.SchemaProperty{
				"text": {Type: "string", Description: "text to echo"},
			},
			Required: []string{"text"},
		},
	}
}
func (runtimeAdapterEchoTool) Execute(_ context.Context, args map[string]any) (string, error) {
	text, _ := args["text"].(string)
	return "tool:" + text, nil
}

type runtimeInternalProviderAdapter struct {
	inner llm.Provider
}

func (p runtimeInternalProviderAdapter) Name() string {
	if p.inner == nil {
		return ""
	}
	return p.inner.Name()
}

func (p runtimeInternalProviderAdapter) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResult, error) {
	resp, err := p.inner.Complete(ctx, &llm.CompletionRequest{
		Model:       req.Model,
		Messages:    runtimeToInternalMessages(req.Messages),
		Tools:       runtimeToInternalTools(req.Tools),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		System:      req.System,
	})
	if err != nil {
		return nil, &ProviderError{Err: err}
	}

	result := &GenerateResult{
		Content:   resp.Content,
		ToolCalls: runtimeToAgentcoreToolCalls(resp.ToolCalls),
		Usage: Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
	}
	switch resp.FinishReason {
	case "", "stop":
		result.FinishReason = FinishFinal
	case "tool_calls":
		result.FinishReason = FinishToolCalls
	case "length":
		return nil, &ProviderError{Err: fmt.Errorf("unsupported finish reason: %s", resp.FinishReason)}
	default:
		return nil, &ProviderError{Err: fmt.Errorf("unknown finish reason: %s", resp.FinishReason)}
	}
	if err := result.Validate(); err != nil {
		return nil, &ProviderError{Err: err}
	}
	return result, nil
}

type runtimeInternalToolsAdapter struct {
	inner *internaltoolsimpl.Registry
}

func (r runtimeInternalToolsAdapter) ListTools(context.Context) ([]ToolSchema, error) {
	if r.inner == nil {
		return nil, nil
	}
	schemas := r.inner.Schemas()
	out := make([]ToolSchema, len(schemas))
	for i, schema := range schemas {
		out[i] = ToolSchema{
			Name:        schema.Name,
			Description: schema.Description,
			InputSchema: runtimeFromSchemaProperty(schema.Parameters),
		}
	}
	return out, nil
}

func (r runtimeInternalToolsAdapter) ExecuteTool(ctx context.Context, call ToolCall) (*ToolResult, error) {
	content, err := r.inner.Execute(ctx, call.Name, cloneMetadata(call.Arguments))
	if err != nil {
		return nil, err
	}
	return &ToolResult{
		CallID:  call.CallID,
		Name:    call.Name,
		Content: content,
	}, nil
}

func runtimeToInternalMessages(messages []Message) []llm.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]llm.Message, len(messages))
	for i, msg := range messages {
		out[i] = llm.Message{
			Role:    msg.Role,
			Content: msg.Content,
		}
		if msg.ToolCall != nil {
			out[i].ToolCall = &llm.ToolCall{
				ID:        msg.ToolCall.CallID,
				Name:      msg.ToolCall.Name,
				Arguments: cloneMetadata(msg.ToolCall.Arguments),
			}
		}
		if msg.ToolResult != nil {
			callID := msg.ToolResult.CallID
			if callID == "" && msg.ToolCall != nil {
				callID = msg.ToolCall.CallID
			}
			out[i].ToolResult = &llm.ToolResult{
				ToolCallID: callID,
				Content:    msg.ToolResult.Content,
			}
		}
	}
	return out
}

func runtimeToInternalTools(tools []ToolSchema) []llm.ToolSchema {
	if len(tools) == 0 {
		return nil
	}
	out := make([]llm.ToolSchema, len(tools))
	for i, tool := range tools {
		out[i] = llm.ToolSchema{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  runtimeToSchemaProperty(tool.InputSchema),
		}
	}
	return out
}

func runtimeToSchemaProperty(schema map[string]any) llm.SchemaProperty {
	prop := llm.SchemaProperty{}
	if len(schema) == 0 {
		return prop
	}
	if value, ok := schema["type"].(string); ok {
		prop.Type = value
	}
	if value, ok := schema["description"].(string); ok {
		prop.Description = value
	}
	switch enum := schema["enum"].(type) {
	case []string:
		prop.Enum = append([]string(nil), enum...)
	case []any:
		prop.Enum = make([]string, 0, len(enum))
		for _, item := range enum {
			if text, ok := item.(string); ok {
				prop.Enum = append(prop.Enum, text)
			}
		}
	}
	switch required := schema["required"].(type) {
	case []string:
		prop.Required = append([]string(nil), required...)
	case []any:
		prop.Required = make([]string, 0, len(required))
		for _, item := range required {
			if text, ok := item.(string); ok {
				prop.Required = append(prop.Required, text)
			}
		}
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		prop.Properties = make(map[string]*llm.SchemaProperty, len(properties))
		for name, raw := range properties {
			if nested, ok := raw.(map[string]any); ok {
				child := runtimeToSchemaProperty(nested)
				prop.Properties[name] = &child
			}
		}
	}
	return prop
}

func runtimeFromSchemaProperty(prop llm.SchemaProperty) map[string]any {
	out := map[string]any{}
	if prop.Type != "" {
		out["type"] = prop.Type
	}
	if prop.Description != "" {
		out["description"] = prop.Description
	}
	if len(prop.Enum) > 0 {
		enum := make([]any, len(prop.Enum))
		for i, item := range prop.Enum {
			enum[i] = item
		}
		out["enum"] = enum
	}
	if len(prop.Required) > 0 {
		required := make([]any, len(prop.Required))
		for i, item := range prop.Required {
			required[i] = item
		}
		out["required"] = required
	}
	if len(prop.Properties) > 0 {
		properties := make(map[string]any, len(prop.Properties))
		for name, child := range prop.Properties {
			if child == nil {
				continue
			}
			properties[name] = runtimeFromSchemaProperty(*child)
		}
		out["properties"] = properties
	}
	return out
}

func runtimeToAgentcoreToolCalls(calls []llm.ToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]ToolCall, len(calls))
	for i, call := range calls {
		out[i] = ToolCall{
			CallID:    call.ID,
			Name:      call.Name,
			Arguments: cloneMetadata(call.Arguments),
		}
	}
	return out
}

func TestRuntime_Run_ResumeFromState_WithFilesStorageAndAdapters(t *testing.T) {
	runID := "resume-with-adapters"
	store := newDefaultFileStorage(t.TempDir())

	reg := internaltoolsimpl.NewRegistry()
	reg.MustRegister(runtimeAdapterEchoTool{})

	yieldAfterSecondModel := &runtimeTestHook{}
	beforeModelCalls := 0
	yieldAfterSecondModel.beforeModel = func(ctrl *Control) {
		beforeModelCalls++
		if beforeModelCalls == 2 {
			ctrl.Yield("await_resume", map[string]any{"step": "after_tool"})
		}
	}

	firstRuntime := New(Config{
		Provider: runtimeInternalProviderAdapter{
			inner: llm.NewMockProvider("internal-mock").
				QueueToolCall("echo", map[string]any{"text": "persisted"}),
		},
		Storage:        store,
		Tools:          runtimeInternalToolsAdapter{inner: reg},
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

	resumeProvider := llm.NewMockProvider("internal-mock").QueueText("final after resume")
	resumeRuntime := New(Config{
		Provider:       runtimeInternalProviderAdapter{inner: resumeProvider},
		Storage:        store,
		Tools:          runtimeInternalToolsAdapter{inner: reg},
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

	providerCalls := resumeProvider.Calls()
	if len(providerCalls) != 1 {
		t.Fatalf("expected one resumed provider call, got %d", len(providerCalls))
	}
	if len(providerCalls[0].Messages) != 3 {
		t.Fatalf("expected resumed provider to receive prior user, assistant tool-call, and tool messages, got %#v", providerCalls[0].Messages)
	}
	if providerCalls[0].Messages[1].Role != "assistant" || providerCalls[0].Messages[1].ToolCall == nil || providerCalls[0].Messages[1].ToolCall.ID != "call-echo" {
		t.Fatalf("expected resumed provider to receive assistant tool-call message, got %#v", providerCalls[0].Messages[1])
	}
	if providerCalls[0].Messages[2].ToolResult == nil || providerCalls[0].Messages[2].ToolResult.Content != "tool:persisted" {
		t.Fatalf("expected resumed provider to receive persisted tool result, got %#v", providerCalls[0].Messages[2])
	}
	if len(providerCalls[0].Tools) != 1 || providerCalls[0].Tools[0].Name != "echo" {
		t.Fatalf("expected resumed provider to receive adapted tool schema, got %#v", providerCalls[0].Tools)
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
		Tools:          NewToolRegistry(),
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
		Tools:          NewToolRegistry(),
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

func TestRuntime_Run_ToolCallThenFinal(t *testing.T) {
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tool := &runtimeTestTool{
		name: "lookup",
		result: &ToolResult{
			Content: "tool-output",
		},
	}
	tools.Register(tool)
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID: "call-1",
				Name:   "lookup",
				Arguments: map[string]any{
					"query": "weather",
				},
			}},
			FinishReason: FinishToolCalls,
			Usage:        Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5},
		}, {
			Content:      "final answer",
			FinishReason: FinishFinal,
			Usage:        Usage{InputTokens: 4, OutputTokens: 3, TotalTokens: 7},
		}},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:       "run-tool-calls",
		AgentID:     "agent-1",
		Instruction: "use a tool",
	})
	if err != nil {
		t.Fatalf("expected tool loop to reach final answer, got %v", err)
	}
	if result.Status != RunCompleted || result.Output != "final answer" {
		t.Fatalf("expected final completed result, got %#v", result)
	}
	if result.Steps != 2 {
		t.Fatalf("expected two model steps, got %#v", result)
	}
	if len(provider.calls) != 2 {
		t.Fatalf("expected two model calls, got %d", len(provider.calls))
	}
	if len(provider.calls[1].Messages) != 3 {
		t.Fatalf("expected second model call to include user, assistant tool-call, and tool messages, got %#v", provider.calls[1].Messages)
	}
	callMsg := provider.calls[1].Messages[1]
	if callMsg.Role != "assistant" || callMsg.ToolCall == nil || callMsg.ToolCall.Name != "lookup" {
		t.Fatalf("expected assistant tool-call message in second model call, got %#v", callMsg)
	}
	toolMsg := provider.calls[1].Messages[2]
	if toolMsg.Role != "tool" || toolMsg.ToolCall == nil || toolMsg.ToolCall.Name != "lookup" || toolMsg.ToolResult == nil || toolMsg.ToolResult.Content != "tool-output" {
		t.Fatalf("expected tool result message in second model call, got %#v", toolMsg)
	}
	saved := store.runs["run-tool-calls"]
	if saved == nil || saved.Status != RunCompleted {
		t.Fatalf("expected completed state after tool loop, got %#v", saved)
	}
	if len(store.messages["run-tool-calls"]) != 4 {
		t.Fatalf("expected persisted messages to include user, assistant tool-call, tool, assistant, got %#v", store.messages["run-tool-calls"])
	}
	if store.messages["run-tool-calls"][1].Role != "assistant" || store.messages["run-tool-calls"][1].ToolCall == nil {
		t.Fatalf("expected persisted assistant tool-call message, got %#v", store.messages["run-tool-calls"][1])
	}
	if store.messages["run-tool-calls"][2].Role != "tool" || store.messages["run-tool-calls"][2].ToolResult == nil {
		t.Fatalf("expected persisted tool message, got %#v", store.messages["run-tool-calls"][2])
	}
}

func TestRuntime_Run_ToolErrorBecomesMessage(t *testing.T) {
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{
		name: "lookup",
		err:  errors.New("lookup exploded"),
	})
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID: "call-1",
				Name:   "lookup",
			}},
			FinishReason: FinishToolCalls,
		}, {
			Content:      "recovered after tool error",
			FinishReason: FinishFinal,
		}},
	}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:       "run-tool-error",
		AgentID:     "agent-1",
		Instruction: "use failing tool",
	})
	if err != nil {
		t.Fatalf("expected tool error to become a message and continue, got %v", err)
	}
	if result.Status != RunCompleted || result.Output != "recovered after tool error" {
		t.Fatalf("expected final result after tool error recovery, got %#v", result)
	}
	if len(provider.calls) != 2 {
		t.Fatalf("expected second model call after tool failure, got %d calls", len(provider.calls))
	}
	if len(provider.calls[1].Messages) != 3 {
		t.Fatalf("expected second model call to include user, assistant tool-call, and tool messages, got %#v", provider.calls[1].Messages)
	}
	if provider.calls[1].Messages[1].Role != "assistant" || provider.calls[1].Messages[1].ToolCall == nil || provider.calls[1].Messages[1].ToolCall.Name != "lookup" {
		t.Fatalf("expected assistant tool-call message before tool error result, got %#v", provider.calls[1].Messages[1])
	}
	toolMsg := provider.calls[1].Messages[2]
	if toolMsg.Role != "tool" || toolMsg.ToolResult == nil || !strings.Contains(toolMsg.ToolResult.Content, "lookup exploded") {
		t.Fatalf("expected tool failure to be turned into tool message, got %#v", toolMsg)
	}
	saved := store.runs["run-tool-error"]
	if saved == nil || saved.Status != RunCompleted || saved.LastError != "" {
		t.Fatalf("expected tool error not to force terminal failure, got %#v", saved)
	}
}

func TestRuntime_Run_MaxStepsExceededFailsRun(t *testing.T) {
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{name: "lookup"})
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID: "call-1",
				Name:   "lookup",
			}},
			FinishReason: FinishToolCalls,
		}, {
			Content:      "should-not-complete",
			FinishReason: FinishFinal,
		}},
	}

	_, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		MaxSteps:       5,
	}).Run(context.Background(), RunInput{
		RunID:       "run-max-steps",
		AgentID:     "agent-1",
		Instruction: "limited",
		MaxSteps:    1,
	})
	if err == nil || !strings.Contains(err.Error(), "max steps") {
		t.Fatalf("expected max-steps failure, got %v", err)
	}
	saved := store.runs["run-max-steps"]
	if saved == nil || saved.Status != RunFailed || !strings.Contains(saved.LastError, "max steps") {
		t.Fatalf("expected failed state with max-steps error, got %#v", saved)
	}
}

func TestRuntime_Run_MaxStepsUsesConfigWhenInputUnset(t *testing.T) {
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{name: "lookup"})
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{{
				CallID: "call-1",
				Name:   "lookup",
			}},
			FinishReason: FinishToolCalls,
		}, {
			Content:      "should-not-complete",
			FinishReason: FinishFinal,
		}},
	}

	_, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		MaxSteps:       1,
	}).Run(context.Background(), RunInput{
		RunID:       "run-max-steps-config",
		AgentID:     "agent-1",
		Instruction: "limited by config",
	})
	if err == nil || !strings.Contains(err.Error(), "max steps") {
		t.Fatalf("expected config max-steps failure when input unset, got %v", err)
	}
	saved := store.runs["run-max-steps-config"]
	if saved == nil || saved.Status != RunFailed || !strings.Contains(saved.LastError, "max steps") {
		t.Fatalf("expected failed state with config max-steps error, got %#v", saved)
	}
}

func TestRuntime_Run_MaxToolCallsPerStepExceededFailsRun(t *testing.T) {
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{name: "lookup"})
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			ToolCalls: []ToolCall{
				{CallID: "call-1", Name: "lookup"},
				{CallID: "call-2", Name: "lookup"},
			},
			FinishReason: FinishToolCalls,
		}},
	}

	_, err := New(Config{
		Provider:            provider,
		Storage:             store,
		Tools:               tools,
		PromptBuilder:       runtimeTestPromptBuilder{},
		ContextBuilder:      runtimeTestContextBuilder{},
		MaxToolCallsPerStep: 1,
	}).Run(context.Background(), RunInput{
		RunID:       "run-max-tool-calls",
		AgentID:     "agent-1",
		Instruction: "too many tools",
	})
	if err == nil || !strings.Contains(err.Error(), "max tool calls") {
		t.Fatalf("expected max-tool-calls failure, got %v", err)
	}
	saved := store.runs["run-max-tool-calls"]
	if saved == nil || saved.Status != RunFailed || !strings.Contains(saved.LastError, "max tool calls") {
		t.Fatalf("expected failed state with max-tool-calls error, got %#v", saved)
	}
}

func TestRuntime_Run_ToolArtifactsPersisted(t *testing.T) {
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{
		name: "lookup",
		result: &ToolResult{
			Content: "tool-output",
			Artifacts: []Artifact{{
				Kind: "file",
				URI:  "artifact://report.txt",
			}},
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

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          tools,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{
		RunID:       "run-tool-artifacts",
		AgentID:     "agent-1",
		Instruction: "collect artifact",
	})
	if err != nil {
		t.Fatalf("expected artifact-producing tool loop to succeed, got %v", err)
	}
	if result.Status != RunCompleted {
		t.Fatalf("expected completed result, got %#v", result)
	}
	saved := store.runs["run-tool-artifacts"]
	if saved == nil || len(saved.Artifacts) != 1 || saved.Artifacts[0].URI != "artifact://report.txt" {
		t.Fatalf("expected tool artifacts to be persisted on run state, got %#v", saved)
	}
}

func TestRuntime_Run_AfterToolRunsBeforeSuccessfulToolPersist(t *testing.T) {
	store := newRuntimeTestStorage()
	afterToolObserved := false
	store.appendHook = func(_ string, msgs []Message) {
		if len(msgs) == 2 && msgs[0].Role == "assistant" && msgs[1].Role == "tool" && !afterToolObserved {
			t.Fatalf("expected successful tool persist to happen after AfterTool hook")
		}
	}

	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{
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
		Tools:          tools,
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
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{
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
		Tools:          tools,
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
	tools := NewToolRegistry()
	tool := &runtimeTestTool{
		name: "lookup",
		result: &ToolResult{
			Content: "tool-output",
		},
	}
	tools.Register(tool)
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
		Tools:          tools,
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
		tools := NewToolRegistry()
		tool := &runtimeTestTool{name: "lookup"}
		tools.Register(tool)
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
			Tools:          tools,
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
		tools := NewToolRegistry()
		tool := &runtimeTestTool{
			name: "lookup",
			result: &ToolResult{
				Content: "tool-output",
			},
		}
		tools.Register(tool)
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
			Tools:          tools,
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
		Tools:          NewToolRegistry(),
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
		Tools:          NewToolRegistry(),
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
		Tools:          NewToolRegistry(),
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
		Tools:          NewToolRegistry(),
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
		Tools:          NewToolRegistry(),
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

func TestRuntime_Run_AfterToolErrorStillPersistsToolMessage(t *testing.T) {
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{
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
		Tools:          tools,
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
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{
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
		Tools:          tools,
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

func TestRuntime_Run_AfterToolErrorIsNotOverriddenByEarlierControl(t *testing.T) {
	store := newRuntimeTestStorage()
	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{
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
		Tools:          tools,
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

	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{
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
		Tools:          tools,
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

	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{
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
		Tools:          tools,
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

	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{
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
		Tools:          tools,
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

	tools := NewToolRegistry()
	tools.Register(&runtimeTestTool{
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
		Tools:          tools,
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
