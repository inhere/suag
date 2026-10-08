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

func TestGenerateResultValidate_AllowsTextWithToolCalls(t *testing.T) {
	res := &GenerateResult{
		Content:      "done",
		ToolCalls:    []ToolCall{{CallID: "call-1", Name: "echo"}},
		FinishReason: FinishToolCalls,
	}

	if err := res.Validate(); err != nil {
		t.Fatalf("text alongside tool calls must be valid, got %v", err)
	}
	res.FinishReason = FinishFinal
	if err := res.Validate(); !errors.Is(err, ErrInvalidFinishReason) {
		t.Fatalf("tool calls with final finish reason must be rejected, got %v", err)
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

func TestDefaultStorage_UsesFilesStorage(t *testing.T) {
	if got := fmt.Sprintf("%T", defaultStorage(nil)); got != "*files.Storage" {
		t.Fatalf("expected default storage to use files.Storage, got %s", got)
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

type runtimeTestStreamingProvider struct {
	runtimeTestProvider
	streamEvents []StreamEvent
	streamCalls  int
}

func (p *runtimeTestStreamingProvider) Stream(ctx context.Context, req *GenerateRequest, emit StreamFunc) (*GenerateResult, error) {
	p.streamCalls++
	p.calls = append(p.calls, cloneGenerateRequest(req))

	for _, event := range p.streamEvents {
		if emit == nil {
			continue
		}
		if err := emit(ctx, event); err != nil {
			return nil, err
		}
	}

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

type runtimeTestToolWithSchema struct {
	runtimeTestTool
	inputSchema map[string]any
}

func (t *runtimeTestToolWithSchema) Schema() ToolSchema {
	return ToolSchema{
		Name:        t.name,
		Description: t.Description(),
		InputSchema: cloneMetadata(t.inputSchema),
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
	runs              map[string]*RunState
	messages          map[string][]Message
	artifacts         map[string][]Artifact
	loadRunErr        error
	loadMsgsErr       error
	saveRunErr        error
	appendErr         error
	artifactLoadErr   error
	artifactSaveErr   error
	appendErrOnCall   int
	appendCalls       int
	artifactLoadCalls int
	artifactSaveCalls int
	appendHook        func(string, []Message)
}

func newRuntimeTestStorage() *runtimeTestStorage {
	return &runtimeTestStorage{
		runs:      make(map[string]*RunState),
		messages:  make(map[string][]Message),
		artifacts: make(map[string][]Artifact),
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

func (s *runtimeTestStorage) SaveArtifacts(_ context.Context, runID string, artifacts []Artifact) error {
	s.artifactSaveCalls++
	if s.artifactSaveErr != nil {
		return s.artifactSaveErr
	}
	s.artifacts[runID] = append([]Artifact(nil), artifacts...)
	return nil
}

func (s *runtimeTestStorage) LoadArtifacts(_ context.Context, runID string) ([]Artifact, error) {
	s.artifactLoadCalls++
	if s.artifactLoadErr != nil {
		return nil, s.artifactLoadErr
	}
	if artifacts, ok := s.artifacts[runID]; ok {
		return append([]Artifact(nil), artifacts...), nil
	}
	return nil, os.ErrNotExist
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

func TestRuntime_Run_UsesGenerateWhenProviderDoesNotStream(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{
		results: []*GenerateResult{{
			Content:      "done",
			FinishReason: FinishFinal,
		}},
	}

	rt := New(Config{Provider: provider, Storage: store})

	result, err := rt.Run(context.Background(), RunInput{
		RunID:       "run-non-stream",
		AgentID:     "agent-1",
		Instruction: "hello",
		Stream: func(context.Context, StreamEvent) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Output != "done" {
		t.Fatalf("expected final output done, got %#v", result)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected generate path to be used once, got %d calls", len(provider.calls))
	}
}

func TestRuntime_Run_UsesGenerateWithoutStreamHandler(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestStreamingProvider{
		runtimeTestProvider: runtimeTestProvider{
			results: []*GenerateResult{{
				Content:      "done",
				FinishReason: FinishFinal,
			}},
		},
	}

	rt := New(Config{Provider: provider, Storage: store})

	result, err := rt.Run(context.Background(), RunInput{
		RunID:       "run-generate-fallback",
		AgentID:     "agent-1",
		Instruction: "hello",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Output != "done" {
		t.Fatalf("expected final output done, got %#v", result)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected one provider call, got %d", len(provider.calls))
	}
	if provider.streamCalls != 0 {
		t.Fatalf("expected stream path to stay unused, got %d calls", provider.streamCalls)
	}
}

func TestRuntime_Run_StreamsTextDeltasAndReturnsFinalResult(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestStreamingProvider{
		runtimeTestProvider: runtimeTestProvider{
			results: []*GenerateResult{{
				Content:      "hello",
				FinishReason: FinishFinal,
			}},
		},
		streamEvents: []StreamEvent{
			{Kind: StreamEventTextDelta, Delta: "hel"},
			{Kind: StreamEventTextDelta, Delta: "lo"},
			{Kind: StreamEventMessageStop},
		},
	}

	var seen []StreamEvent
	rt := New(Config{Provider: provider, Storage: store})

	result, err := rt.Run(context.Background(), RunInput{
		RunID:       "run-stream",
		AgentID:     "agent-1",
		Instruction: "hello",
		Stream: func(_ context.Context, event StreamEvent) error {
			seen = append(seen, event)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Output != "hello" {
		t.Fatalf("expected final output hello, got %#v", result)
	}
	if provider.streamCalls != 1 {
		t.Fatalf("expected one stream call, got %d", provider.streamCalls)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected one provider request, got %d", len(provider.calls))
	}
	if len(seen) != 3 {
		t.Fatalf("expected 3 stream events, got %#v", seen)
	}
}

func TestRuntime_Run_StreamHandlerErrorFailsRun(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestStreamingProvider{
		runtimeTestProvider: runtimeTestProvider{
			results: []*GenerateResult{{
				Content:      "hello",
				FinishReason: FinishFinal,
			}},
		},
		streamEvents: []StreamEvent{
			{Kind: StreamEventTextDelta, Delta: "hel"},
		},
	}

	streamErr := errors.New("stream handler failed")
	rt := New(Config{Provider: provider, Storage: store})

	_, err := rt.Run(context.Background(), RunInput{
		RunID:       "run-stream-error",
		AgentID:     "agent-1",
		Instruction: "hello",
		Stream: func(context.Context, StreamEvent) error {
			return streamErr
		},
	})
	if !errors.Is(err, streamErr) {
		t.Fatalf("expected stream handler error, got %v", err)
	}
}

func TestRuntime_Run_StreamingStillPersistsOnlyFinalAssistantMessage(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestStreamingProvider{
		runtimeTestProvider: runtimeTestProvider{
			results: []*GenerateResult{{
				Content:      "hello",
				FinishReason: FinishFinal,
			}},
		},
		streamEvents: []StreamEvent{
			{Kind: StreamEventTextDelta, Delta: "hel"},
			{Kind: StreamEventTextDelta, Delta: "lo"},
			{Kind: StreamEventMessageStop},
		},
	}

	rt := New(Config{Provider: provider, Storage: store})

	result, err := rt.Run(context.Background(), RunInput{
		RunID:       "run-stream-persist",
		AgentID:     "agent-1",
		Instruction: "hello",
		Stream: func(context.Context, StreamEvent) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	msgs := store.messages[result.RunID]
	if len(msgs) != 2 {
		t.Fatalf("expected only user and final assistant messages to persist, got %#v", msgs)
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "hello" {
		t.Fatalf("expected final assistant message to persist, got %#v", msgs[1])
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
