package agentcore

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Runtime executes agent runs using the public agentcore contracts.
type Runtime interface {
	Run(ctx context.Context, input RunInput) (*RunResult, error)
}

// Config configures the default runtime implementation.
type Config struct {
	Provider            Provider
	Storage             Storage
	Tools               ToolRegistry
	PromptBuilder       PromptBuilder
	ContextBuilder      ContextBuilder
	Hooks               []Hook
	MaxSteps            int
	MaxToolCallsPerStep int
}

// DefaultRuntime provides the minimal public runtime orchestration.
type DefaultRuntime struct {
	provider            Provider
	storage             Storage
	tools               ToolRegistry
	promptBuilder       PromptBuilder
	contextBuilder      ContextBuilder
	hooks               runtimeHookChain
	maxSteps            int
	maxToolCallsPerStep int
}

// New constructs a runnable runtime, filling in built-in defaults when possible.
func New(cfg Config) *DefaultRuntime {
	return &DefaultRuntime{
		provider:            cfg.Provider,
		storage:             defaultStorage(cfg.Storage),
		tools:               defaultTools(cfg.Tools),
		promptBuilder:       defaultPromptBuilder(cfg.PromptBuilder),
		contextBuilder:      defaultContextBuilder(cfg.ContextBuilder),
		hooks:               newRuntimeHookChain(cfg.Hooks...),
		maxSteps:            cfg.MaxSteps,
		maxToolCallsPerStep: cfg.MaxToolCallsPerStep,
	}
}

// Run executes the minimal final-answer runtime path.
func (r *DefaultRuntime) Run(ctx context.Context, input RunInput) (*RunResult, error) {
	if r == nil || r.provider == nil {
		return nil, ErrProviderRequired
	}

	state, appendInstruction, err := r.prepareState(ctx, input)
	if err != nil {
		return nil, err
	}

	ctrl := NewControl()
	runEvent := &RunEvent{
		RunID:     state.RunID,
		AgentID:   state.AgentID,
		Input:     cloneRunInput(input),
		Result:    nil,
		Metadata:  cloneMetadata(state.Metadata),
		Timestamp: time.Now(),
	}

	if err := r.runBeforeRun(ctx, runEvent, ctrl); err != nil {
		return nil, r.handleError(ctx, state, "before_run", err, ctrl)
	}

	newMessages := make([]Message, 0, 2)
	if appendInstruction {
		userMessage := Message{Role: "user", Content: input.Instruction}
		state.Messages = append(state.Messages, userMessage)
		newMessages = append(newMessages, userMessage)
	}

	if err := r.saveState(ctx, state, newMessages); err != nil {
		return nil, r.handleError(ctx, state, "persist_input", err, ctrl)
	}

	systemPrompt, err := r.buildPrompt(ctx, state, ctrl)
	if err != nil {
		return nil, r.handleError(ctx, state, "prompt", err, ctrl)
	}

	messages, err := r.buildContext(ctx, state, ctrl)
	if err != nil {
		return nil, r.handleError(ctx, state, "context", err, ctrl)
	}

	tools, err := r.listTools(ctx)
	if err != nil {
		return nil, r.handleError(ctx, state, "tools", err, ctrl)
	}

	req := &GenerateRequest{
		System:   systemPrompt,
		Messages: messages,
		Tools:    tools,
		Metadata: cloneMetadata(state.Metadata),
	}

	res, err := r.callModel(ctx, state, req, ctrl)
	if err != nil {
		return nil, r.handleError(ctx, state, "model", err, ctrl)
	}

	state.Step++
	state.Usage = res.Usage

	if res.FinishReason == FinishToolCalls {
		return nil, r.handleError(ctx, state, "tool_loop", ErrToolCallsNotImplemented, ctrl)
	}
	if res.FinishReason != FinishFinal {
		return nil, r.handleError(ctx, state, "model", ErrInvalidFinishReason, ctrl)
	}

	assistantMessage := Message{Role: "assistant", Content: res.Content}
	state.Messages = append(state.Messages, assistantMessage)
	state.Status = RunCompleted
	state.LastError = ""

	if err := r.saveState(ctx, state, []Message{assistantMessage}); err != nil {
		return nil, r.handleError(ctx, state, "persist_output", err, ctrl)
	}

	result := &RunResult{
		RunID:  state.RunID,
		Status: RunCompleted,
		Output: res.Content,
		Steps:  state.Step,
		Usage:  state.Usage,
	}
	runEvent.Result = result
	runEvent.Metadata = cloneMetadata(state.Metadata)

	if err := r.runAfterRun(ctx, runEvent, ctrl); err != nil {
		return nil, r.handleError(ctx, state, "after_run", err, ctrl)
	}

	return result, nil
}

func (r *DefaultRuntime) prepareState(ctx context.Context, input RunInput) (*RunState, bool, error) {
	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		runID = newRunID()
	}

	if strings.TrimSpace(input.AgentID) == "" {
		input.AgentID = "agentcore"
	}

	if strings.TrimSpace(input.RunID) == "" {
		return newRunState(runID, input), true, nil
	}

	loaded, err := r.storage.LoadRun(ctx, runID)
	if err != nil {
		if isStorageMiss(err) {
			return newRunState(runID, input), true, nil
		}
		return nil, false, wrapStorageError(err)
	}
	if loaded == nil {
		return newRunState(runID, input), true, nil
	}

	if loaded.Status == RunCompleted || loaded.Status == RunFailed {
		return nil, false, fmt.Errorf("%w: status=%s", ErrRunNotResumable, loaded.Status)
	}

	messages, err := r.storage.LoadMessages(ctx, runID)
	if err != nil {
		return nil, false, wrapStorageError(err)
	}

	state := cloneRunStateValue(loaded)
	state.Messages = messages

	appendInstruction := shouldAppendInstruction(input)
	if state.AgentID == "" {
		state.AgentID = input.AgentID
	}
	if len(input.Metadata) > 0 {
		state.Metadata = mergeMetadata(state.Metadata, input.Metadata)
	}

	return state, appendInstruction, nil
}

func (r *DefaultRuntime) runBeforeRun(ctx context.Context, event *RunEvent, ctrl *Control) error {
	if err := r.hooks.BeforeRun(ctx, event, ctrl); err != nil {
		return wrapHookError(err)
	}
	return nil
}

func (r *DefaultRuntime) runAfterRun(ctx context.Context, event *RunEvent, ctrl *Control) error {
	if err := r.hooks.AfterRun(ctx, event, ctrl); err != nil {
		return wrapHookError(err)
	}
	return nil
}

func (r *DefaultRuntime) buildPrompt(ctx context.Context, state *RunState, ctrl *Control) (string, error) {
	systemPrompt, err := r.promptBuilder.BuildSystemPrompt(ctx, state)
	if err != nil {
		return "", err
	}

	event := &PromptEvent{
		RunID:     state.RunID,
		AgentID:   state.AgentID,
		State:     state,
		System:    systemPrompt,
		Metadata:  cloneMetadata(state.Metadata),
		Timestamp: time.Now(),
	}
	if err := r.hooks.BeforePromptBuild(ctx, event, ctrl); err != nil {
		return "", wrapHookError(err)
	}
	return event.System, nil
}

func (r *DefaultRuntime) buildContext(ctx context.Context, state *RunState, ctrl *Control) ([]Message, error) {
	messages, err := r.contextBuilder.BuildMessages(ctx, state)
	if err != nil {
		return nil, err
	}

	event := &ContextEvent{
		RunID:     state.RunID,
		AgentID:   state.AgentID,
		State:     state,
		Messages:  cloneMessagesValue(messages),
		Metadata:  cloneMetadata(state.Metadata),
		Timestamp: time.Now(),
	}
	if err := r.hooks.BeforeContextBuild(ctx, event, ctrl); err != nil {
		return nil, wrapHookError(err)
	}
	return event.Messages, nil
}

func (r *DefaultRuntime) listTools(ctx context.Context) ([]ToolSchema, error) {
	tools, err := r.tools.ListTools(ctx)
	if err != nil {
		var toolErr *ToolError
		if errors.As(err, &toolErr) {
			return nil, err
		}
		return nil, &ToolError{Err: err}
	}
	return tools, nil
}

func (r *DefaultRuntime) callModel(ctx context.Context, state *RunState, req *GenerateRequest, ctrl *Control) (*GenerateResult, error) {
	event := &ModelEvent{
		RunID:     state.RunID,
		AgentID:   state.AgentID,
		Request:   cloneGenerateRequestValue(req),
		Metadata:  cloneMetadata(state.Metadata),
		Timestamp: time.Now(),
	}
	if err := r.hooks.BeforeModel(ctx, event, ctrl); err != nil {
		return nil, wrapHookError(err)
	}

	result, err := r.provider.Generate(ctx, event.Request)
	if err != nil {
		return nil, wrapProviderError(err)
	}
	if err := result.Validate(); err != nil {
		return nil, wrapProviderError(err)
	}

	event.Result = cloneGenerateResultValue(result)
	if err := r.hooks.AfterModel(ctx, event, ctrl); err != nil {
		return nil, wrapHookError(err)
	}
	return event.Result, nil
}

func (r *DefaultRuntime) saveState(ctx context.Context, state *RunState, messages []Message) error {
	if err := r.storage.SaveRun(ctx, state); err != nil {
		return wrapStorageError(err)
	}
	if len(messages) == 0 {
		return nil
	}
	if err := r.storage.AppendMessages(ctx, state.RunID, messages...); err != nil {
		return wrapStorageError(err)
	}
	return nil
}

func (r *DefaultRuntime) handleError(ctx context.Context, state *RunState, stage string, err error, ctrl *Control) error {
	if err == nil {
		return nil
	}

	wrapped := classifyRuntimeError(err)
	event := &ErrorEvent{
		RunID:     state.RunID,
		AgentID:   state.AgentID,
		Stage:     stage,
		Err:       wrapped,
		Metadata:  cloneMetadata(state.Metadata),
		Timestamp: time.Now(),
	}
	if hookErr := r.hooks.OnError(ctx, event, ctrl); hookErr != nil {
		return wrapHookError(hookErr)
	}
	return wrapped
}

func defaultStorage(storage Storage) Storage {
	if storage != nil {
		return storage
	}
	return newDefaultFileStorage(filepath.Join(os.TempDir(), "agentcore-runs"))
}

func defaultTools(tools ToolRegistry) ToolRegistry {
	if tools != nil {
		return tools
	}
	return NewToolRegistry()
}

func defaultPromptBuilder(builder PromptBuilder) PromptBuilder {
	if builder != nil {
		return builder
	}
	return defaultStaticPromptBuilder{}
}

func defaultContextBuilder(builder ContextBuilder) ContextBuilder {
	if builder != nil {
		return builder
	}
	return defaultContextBuilderImpl{}
}

func newRunState(runID string, input RunInput) *RunState {
	return &RunState{
		RunID:    runID,
		AgentID:  input.AgentID,
		Status:   RunRunning,
		Metadata: cloneMetadata(input.Metadata),
	}
}

func shouldAppendInstruction(input RunInput) bool {
	switch input.ResumeMode {
	case ResumeFromState:
		return false
	case ResumeWithInput:
		return strings.TrimSpace(input.Instruction) != ""
	case ResumeAuto, "":
		return strings.TrimSpace(input.Instruction) != ""
	default:
		return strings.TrimSpace(input.Instruction) != ""
	}
}

func newRunID() string {
	return fmt.Sprintf("run-%d", time.Now().UnixNano())
}

func wrapProviderError(err error) error {
	if err == nil {
		return nil
	}
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		return err
	}
	return &ProviderError{Err: err}
}

func wrapStorageError(err error) error {
	if err == nil {
		return nil
	}
	var storageErr *StorageError
	if errors.As(err, &storageErr) {
		return err
	}
	return &StorageError{Err: err}
}

func wrapHookError(err error) error {
	if err == nil {
		return nil
	}
	var hookErr *HookError
	if errors.As(err, &hookErr) {
		return err
	}
	return &HookError{Err: err}
}

func isStorageMiss(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}

func classifyRuntimeError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.As(err, new(*HookError)):
		return err
	case errors.As(err, new(*ProviderError)):
		return err
	case errors.As(err, new(*StorageError)):
		return err
	case errors.As(err, new(*ToolError)):
		return err
	case errors.Is(err, ErrToolCallsNotImplemented):
		return err
	case errors.Is(err, ErrRunNotResumable):
		return err
	default:
		return err
	}
}

func cloneRunInput(input RunInput) *RunInput {
	cloned := input
	cloned.Metadata = cloneMetadata(input.Metadata)
	return &cloned
}

func cloneRunStateValue(state *RunState) *RunState {
	if state == nil {
		return nil
	}
	cloned := *state
	cloned.Messages = cloneMessagesValue(state.Messages)
	if len(state.Artifacts) > 0 {
		cloned.Artifacts = make([]Artifact, len(state.Artifacts))
		copy(cloned.Artifacts, state.Artifacts)
	}
	cloned.Metadata = cloneMetadata(state.Metadata)
	cloned.Scratch = cloneMetadata(state.Scratch)
	return &cloned
}

func cloneMessagesValue(messages []Message) []Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]Message, len(messages))
	for i, msg := range messages {
		cloned[i] = Message{
			Role:       msg.Role,
			Content:    msg.Content,
			Metadata:   cloneMetadata(msg.Metadata),
			ToolCall:   cloneToolCallValue(msg.ToolCall),
			ToolResult: cloneToolResultValue(msg.ToolResult),
		}
	}
	return cloned
}

func cloneToolCallValue(call *ToolCall) *ToolCall {
	if call == nil {
		return nil
	}
	cloned := *call
	cloned.Arguments = cloneMetadata(call.Arguments)
	return &cloned
}

func cloneToolResultValue(result *ToolResult) *ToolResult {
	if result == nil {
		return nil
	}
	cloned := *result
	if len(result.Artifacts) > 0 {
		cloned.Artifacts = make([]Artifact, len(result.Artifacts))
		copy(cloned.Artifacts, result.Artifacts)
	}
	cloned.Metadata = cloneMetadata(result.Metadata)
	return &cloned
}

func cloneGenerateRequestValue(req *GenerateRequest) *GenerateRequest {
	if req == nil {
		return nil
	}
	cloned := *req
	cloned.Messages = cloneMessagesValue(req.Messages)
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
	cloned.Metadata = cloneMetadata(req.Metadata)
	return &cloned
}

func cloneGenerateResultValue(result *GenerateResult) *GenerateResult {
	if result == nil {
		return nil
	}
	cloned := *result
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
	if len(result.Artifacts) > 0 {
		cloned.Artifacts = make([]Artifact, len(result.Artifacts))
		copy(cloned.Artifacts, result.Artifacts)
	}
	cloned.Metadata = cloneMetadata(result.Metadata)
	return &cloned
}

func mergeMetadata(base, overlay map[string]any) map[string]any {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	merged := cloneMetadata(base)
	if merged == nil {
		merged = make(map[string]any, len(overlay))
	}
	for k, v := range overlay {
		merged[k] = v
	}
	return merged
}

type defaultStaticPromptBuilder struct{}

func (defaultStaticPromptBuilder) BuildSystemPrompt(context.Context, *RunState) (string, error) {
	return "", nil
}

type defaultContextBuilderImpl struct{}

func (defaultContextBuilderImpl) BuildMessages(_ context.Context, state *RunState) ([]Message, error) {
	if state == nil || len(state.Messages) == 0 {
		return nil, nil
	}
	return cloneMessagesValue(state.Messages), nil
}

type runtimeHookChain struct {
	hooks []Hook
}

func newRuntimeHookChain(hooks ...Hook) runtimeHookChain {
	filtered := make([]Hook, 0, len(hooks))
	for _, hook := range hooks {
		if hook != nil {
			filtered = append(filtered, hook)
		}
	}
	return runtimeHookChain{hooks: filtered}
}

func (c runtimeHookChain) runStage(call func(Hook) error, ctrl *Control) error {
	for _, hook := range c.hooks {
		if err := call(hook); err != nil {
			return err
		}
		if ctrl != nil && (ctrl.ShouldShortCircuit() || ctrl.ShouldYield()) {
			return nil
		}
	}
	return nil
}

func (c runtimeHookChain) BeforeRun(ctx context.Context, event *RunEvent, ctrl *Control) error {
	return c.runStage(func(h Hook) error { return h.BeforeRun(ctx, event, ctrl) }, ctrl)
}

func (c runtimeHookChain) BeforePromptBuild(ctx context.Context, event *PromptEvent, ctrl *Control) error {
	return c.runStage(func(h Hook) error { return h.BeforePromptBuild(ctx, event, ctrl) }, ctrl)
}

func (c runtimeHookChain) BeforeContextBuild(ctx context.Context, event *ContextEvent, ctrl *Control) error {
	return c.runStage(func(h Hook) error { return h.BeforeContextBuild(ctx, event, ctrl) }, ctrl)
}

func (c runtimeHookChain) BeforeModel(ctx context.Context, event *ModelEvent, ctrl *Control) error {
	return c.runStage(func(h Hook) error { return h.BeforeModel(ctx, event, ctrl) }, ctrl)
}

func (c runtimeHookChain) AfterModel(ctx context.Context, event *ModelEvent, ctrl *Control) error {
	return c.runStage(func(h Hook) error { return h.AfterModel(ctx, event, ctrl) }, ctrl)
}

func (c runtimeHookChain) AfterRun(ctx context.Context, event *RunEvent, ctrl *Control) error {
	return c.runStage(func(h Hook) error { return h.AfterRun(ctx, event, ctrl) }, ctrl)
}

func (c runtimeHookChain) OnError(ctx context.Context, event *ErrorEvent, ctrl *Control) error {
	return c.runStage(func(h Hook) error { return h.OnError(ctx, event, ctrl) }, ctrl)
}

type defaultFileStorage struct {
	root string
}

func newDefaultFileStorage(root string) *defaultFileStorage {
	return &defaultFileStorage{root: root}
}

func (s *defaultFileStorage) LoadRun(_ context.Context, runID string) (*RunState, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, &StorageError{Err: errors.New("run id is required")}
	}
	data, err := os.ReadFile(filepath.Join(s.root, runID, "state.json"))
	if err != nil {
		return nil, &StorageError{Err: err}
	}
	var state RunState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, &StorageError{Err: err}
	}
	return &state, nil
}

func (s *defaultFileStorage) LoadMessages(_ context.Context, runID string) ([]Message, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, &StorageError{Err: errors.New("run id is required")}
	}
	file, err := os.Open(filepath.Join(s.root, runID, "messages.jsonl"))
	if err != nil {
		return nil, &StorageError{Err: err}
	}
	defer file.Close()

	var messages []Message
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			var msg Message
			if unmarshalErr := json.Unmarshal(line, &msg); unmarshalErr != nil {
				return nil, &StorageError{Err: unmarshalErr}
			}
			messages = append(messages, msg)
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			break
		}
		return nil, &StorageError{Err: err}
	}
	if messages == nil {
		return []Message{}, nil
	}
	return messages, nil
}

func (s *defaultFileStorage) SaveRun(_ context.Context, state *RunState) error {
	if state == nil {
		return &StorageError{Err: errors.New("run state is nil")}
	}
	runDir := filepath.Join(s.root, state.RunID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return &StorageError{Err: err}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return &StorageError{Err: err}
	}
	if err := os.WriteFile(filepath.Join(runDir, "state.json"), data, 0o644); err != nil {
		return &StorageError{Err: err}
	}
	return nil
}

func (s *defaultFileStorage) AppendMessages(_ context.Context, runID string, msgs ...Message) error {
	runDir := filepath.Join(s.root, runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return &StorageError{Err: err}
	}
	if len(msgs) == 0 {
		return nil
	}
	file, err := os.OpenFile(filepath.Join(runDir, "messages.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return &StorageError{Err: err}
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	for _, msg := range msgs {
		if err := enc.Encode(msg); err != nil {
			return &StorageError{Err: err}
		}
	}
	return nil
}
