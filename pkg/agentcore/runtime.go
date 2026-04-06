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
	Memory              Memory
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
	memory              Memory
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
		memory:              defaultMemory(cfg.Memory),
		tools:               defaultTools(cfg.Tools),
		promptBuilder:       defaultPromptBuilder(cfg.PromptBuilder),
		contextBuilder:      cfg.ContextBuilder,
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

	if err := r.memory.Hydrate(ctx, cloneRunStateValue(state)); err != nil {
		return r.returnWithHandledError(ctx, input, state, "memory_hydrate", wrapMemoryError(err), ctrl)
	}

	if err := r.runBeforeRun(ctx, runEvent, ctrl); err != nil {
		return r.returnWithHandledError(ctx, input, state, "before_run", err, ctrl)
	}
	if result, err, handled := r.resolveControl(ctx, state, ctrl); handled {
		return r.finishRun(ctx, input, state, result, err)
	}

	newMessages := make([]Message, 0, 2)
	preInputState := cloneRunStateValue(state)
	if appendInstruction {
		userMessage := Message{Role: "user", Content: input.Instruction}
		state.Messages = append(state.Messages, userMessage)
		newMessages = append(newMessages, userMessage)
	}

	if err := r.saveState(ctx, state, newMessages); err != nil {
		return r.returnWithHandledError(ctx, input, preInputState, "persist_input", err, ctrl)
	}

	maxSteps := r.effectiveMaxSteps(input)

	for {
		if maxSteps > 0 && state.Step >= maxSteps {
			return r.returnWithHandledError(ctx, input, state, "max_steps", fmt.Errorf("max steps exceeded: %d", maxSteps), NewControl())
		}

		systemPrompt, err := r.buildPrompt(ctx, state, ctrl)
		if err != nil {
			return r.returnWithHandledError(ctx, input, state, "prompt", err, ctrl)
		}
		if result, err, handled := r.resolveControl(ctx, state, ctrl); handled {
			return r.finishRun(ctx, input, state, result, err)
		}

		messages, err := r.buildContext(ctx, state, ctrl)
		if err != nil {
			return r.returnWithHandledError(ctx, input, state, "context", err, ctrl)
		}
		if result, err, handled := r.resolveControl(ctx, state, ctrl); handled {
			return r.finishRun(ctx, input, state, result, err)
		}

		tools, err := r.listTools(ctx)
		if err != nil {
			return r.returnWithHandledError(ctx, input, state, "tools", err, ctrl)
		}

		req := &GenerateRequest{
			System:   systemPrompt,
			Messages: messages,
			Tools:    tools,
			Metadata: cloneMetadata(state.Metadata),
		}

		res, err := r.callModel(ctx, state, req, ctrl)
		if err != nil {
			return r.returnWithHandledError(ctx, input, state, "model", err, ctrl)
		}
		if res == nil {
			if result, err, handled := r.resolveControl(ctx, state, ctrl); handled {
				return r.finishRun(ctx, input, state, result, err)
			}
			return r.returnWithHandledError(ctx, input, state, "model", fmt.Errorf("model execution returned no result"), ctrl)
		}

		state.Step++
		state.Usage = res.Usage
		if result, err, handled := r.resolveControl(ctx, state, ctrl); handled {
			return r.finishRun(ctx, input, state, result, err)
		}

		switch res.FinishReason {
		case FinishToolCalls:
			if result, err, handled := r.executeToolCalls(ctx, state, res.ToolCalls); handled {
				return r.finishRun(ctx, input, state, result, err)
			}
			continue
		case FinishFinal:
			result, err := r.persistFinalAnswer(ctx, input, state, res.Content)
			if err != nil {
				return nil, err
			}
			return r.finishRun(ctx, input, state, result, nil)
		default:
			return r.returnWithHandledError(ctx, input, state, "model", ErrInvalidFinishReason, ctrl)
		}
	}
}

func (r *DefaultRuntime) returnWithHandledError(ctx context.Context, input RunInput, state *RunState, stage string, err error, ctrl *Control) (*RunResult, error) {
	handledErr := r.handleError(ctx, state, stage, err, ctrl)
	var resolved runtimeResolvedControlError
	if errors.As(handledErr, &resolved) {
		return r.finishRun(ctx, input, state, resolved.result, nil)
	}
	return r.finishRun(ctx, input, state, nil, handledErr)
}

func (r *DefaultRuntime) finishRun(ctx context.Context, input RunInput, state *RunState, result *RunResult, runErr error) (*RunResult, error) {
	afterCtrl := NewControl()
	event := &RunEvent{
		RunID:     state.RunID,
		AgentID:   state.AgentID,
		Input:     cloneRunInput(input),
		Result:    result,
		Metadata:  cloneMetadata(state.Metadata),
		Timestamp: time.Now(),
	}

	if err := r.runAfterRun(ctx, event, afterCtrl); err != nil {
		if runErr != nil {
			return nil, runErr
		}
		handledErr := r.handleError(ctx, state, "after_run", err, afterCtrl)
		var resolved runtimeResolvedControlError
		if errors.As(handledErr, &resolved) {
			return resolved.result, nil
		}
		return nil, handledErr
	}
	if runErr != nil {
		return nil, runErr
	}
	if resolvedResult, resolvedErr, handled := r.resolveControl(ctx, state, afterCtrl); handled {
		return resolvedResult, resolvedErr
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
	event := &PromptEvent{
		RunID:     state.RunID,
		AgentID:   state.AgentID,
		State:     state,
		System:    "",
		Metadata:  cloneMetadata(state.Metadata),
		Timestamp: time.Now(),
	}
	if err := r.hooks.BeforePromptBuild(ctx, event, ctrl); err != nil {
		return "", wrapHookError(err)
	}
	if ctrl != nil && (ctrl.ShouldYield() || ctrl.ShouldShortCircuit()) {
		return event.System, nil
	}
	systemPrompt, err := r.promptBuilder.BuildSystemPrompt(ctx, state)
	if err != nil {
		return "", err
	}
	event.System = systemPrompt
	return event.System, nil
}

func (r *DefaultRuntime) buildContext(ctx context.Context, state *RunState, ctrl *Control) ([]Message, error) {
	event := &ContextEvent{
		RunID:     state.RunID,
		AgentID:   state.AgentID,
		State:     state,
		Messages:  nil,
		Metadata:  cloneMetadata(state.Metadata),
		Timestamp: time.Now(),
	}
	if err := r.hooks.BeforeContextBuild(ctx, event, ctrl); err != nil {
		return nil, wrapHookError(err)
	}
	if ctrl != nil && (ctrl.ShouldYield() || ctrl.ShouldShortCircuit()) {
		return event.Messages, nil
	}
	builder := r.contextBuilder
	if builder == nil {
		builder = defaultContextBuilder(r.memory)
	}
	messages, err := builder.BuildMessages(ctx, state)
	if err != nil {
		return nil, err
	}
	event.Messages = cloneMessagesValue(messages)
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
	if ctrl != nil && (ctrl.ShouldYield() || ctrl.ShouldShortCircuit()) {
		return nil, nil
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
	result = event.Result
	return result, nil
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
	if err := r.memory.ObserveMessages(ctx, cloneRunStateValue(state), cloneMessagesValue(messages)...); err != nil {
		return wrapMemoryError(err)
	}
	return nil
}

func (r *DefaultRuntime) effectiveMaxSteps(input RunInput) int {
	if input.MaxSteps > 0 {
		return input.MaxSteps
	}
	return r.maxSteps
}

func (r *DefaultRuntime) executeToolCalls(ctx context.Context, state *RunState, calls []ToolCall) (*RunResult, error, bool) {
	if limit := r.maxToolCallsPerStep; limit > 0 && len(calls) > limit {
		return r.handleErrorResult(ctx, state, "max_tool_calls", fmt.Errorf("max tool calls per step exceeded: %d > %d", len(calls), limit), NewControl())
	}

	for _, call := range calls {
		result, err, handled := r.executeSingleTool(ctx, state, call)
		if handled {
			return result, err, true
		}
	}
	return nil, nil, false
}

func (r *DefaultRuntime) executeSingleTool(ctx context.Context, state *RunState, call ToolCall) (*RunResult, error, bool) {
	ctrl := NewControl()
	event := &ToolEvent{
		RunID:     state.RunID,
		AgentID:   state.AgentID,
		Call:      cloneToolCallValue(&call),
		Metadata:  cloneMetadata(state.Metadata),
		Timestamp: time.Now(),
	}
	if err := r.hooks.BeforeTool(ctx, event, ctrl); err != nil {
		return r.handleErrorResult(ctx, state, "before_tool", wrapHookError(err), ctrl)
	}
	if result, err, handled := r.resolveControl(ctx, state, ctrl); handled {
		return result, err, true
	}

	activeCall := call
	if event.Call != nil {
		activeCall = *cloneToolCallValue(event.Call)
	}

	result, err := r.tools.ExecuteTool(ctx, activeCall)
	if err != nil {
		result = toolErrorAsResult(activeCall, err)
	} else {
		result = normalizeToolResult(activeCall, result)
	}

	event.Call = cloneToolCallValue(&activeCall)
	event.Result = cloneToolResultValue(result)
	if err := r.hooks.AfterTool(ctx, event, ctrl); err != nil {
		finalCall := activeCall
		if event.Call != nil {
			finalCall = *cloneToolCallValue(event.Call)
		}
		finalResult := normalizeToolResult(finalCall, event.Result)
		if result, persistErr, handled := r.persistToolTranscript(ctx, state, finalCall, finalResult); handled {
			return result, persistErr, true
		}
		return r.handleErrorResult(ctx, state, "after_tool", wrapHookError(err), NewControl())
	}
	finalCall := activeCall
	if event.Call != nil {
		finalCall = *cloneToolCallValue(event.Call)
	}
	finalResult := normalizeToolResult(finalCall, event.Result)
	if ctrl.ShouldYield() || ctrl.ShouldShortCircuit() {
		if result, persistErr, handled := r.persistToolTranscript(ctx, state, finalCall, finalResult); handled {
			return result, persistErr, true
		}
		return r.handleErrorResult(ctx, state, "after_tool", wrapHookError(errors.New("after_tool control is not supported")), NewControl())
	}

	if result, err, handled := r.persistToolTranscript(ctx, state, finalCall, finalResult); handled {
		return result, err, true
	}
	return nil, nil, false
}

func (r *DefaultRuntime) persistToolTranscript(ctx context.Context, state *RunState, call ToolCall, result *ToolResult) (*RunResult, error, bool) {
	assistantMessage := Message{
		Role:     "assistant",
		ToolCall: cloneToolCallValue(&call),
	}
	toolMessage := Message{
		Role:       "tool",
		Content:    result.Content,
		ToolCall:   cloneToolCallValue(&call),
		ToolResult: cloneToolResultValue(result),
	}
	state.Messages = append(state.Messages, assistantMessage, toolMessage)
	if len(result.Artifacts) > 0 {
		state.Artifacts = append(state.Artifacts, result.Artifacts...)
	}

	if err := r.saveState(ctx, state, []Message{assistantMessage, toolMessage}); err != nil {
		return nil, r.handleNonRecoverableError(ctx, state, "persist_tool_output", err), true
	}
	return nil, nil, false
}

func (r *DefaultRuntime) persistFinalAnswer(ctx context.Context, input RunInput, state *RunState, content string) (*RunResult, error) {
	preOutputState := cloneRunStateValue(state)
	assistantMessage := Message{Role: "assistant", Content: content}
	state.Messages = append(state.Messages, assistantMessage)
	state.Status = RunCompleted
	state.LastError = ""

	if err := r.saveState(ctx, state, []Message{assistantMessage}); err != nil {
		return r.returnWithHandledError(ctx, input, preOutputState, "persist_output", err, NewControl())
	}

	return &RunResult{
		RunID:  state.RunID,
		Status: RunCompleted,
		Output: content,
		Steps:  state.Step,
		Usage:  state.Usage,
	}, nil
}

func (r *DefaultRuntime) handleErrorResult(ctx context.Context, state *RunState, stage string, err error, ctrl *Control) (*RunResult, error, bool) {
	handledErr := r.handleError(ctx, state, stage, err, ctrl)
	var resolved runtimeResolvedControlError
	if errors.As(handledErr, &resolved) {
		return resolved.result, nil, true
	}
	return nil, handledErr, true
}

func (r *DefaultRuntime) handleNonRecoverableError(ctx context.Context, state *RunState, stage string, err error) error {
	if err == nil {
		return nil
	}

	wrapped := classifyRuntimeError(err)
	if persistErr := r.persistFailureState(ctx, state, wrapped); persistErr != nil {
		wrapped = persistErr
	}
	event := &ErrorEvent{
		RunID:     state.RunID,
		AgentID:   state.AgentID,
		Stage:     stage,
		Err:       wrapped,
		Metadata:  cloneMetadata(state.Metadata),
		Timestamp: time.Now(),
	}
	_ = r.hooks.OnErrorObserved(ctx, event, NewControl())
	return wrapped
}

func (r *DefaultRuntime) resolveControl(ctx context.Context, state *RunState, ctrl *Control) (*RunResult, error, bool) {
	if ctrl == nil {
		return nil, nil, false
	}
	if ctrl.ShouldYield() {
		result, err := r.persistYield(ctx, state, ctrl)
		return result, err, true
	}
	if ctrl.ShouldShortCircuit() {
		result, err := r.persistShortCircuit(ctx, state, ctrl)
		return result, err, true
	}
	return nil, nil, false
}

func (r *DefaultRuntime) handleError(ctx context.Context, state *RunState, stage string, err error, ctrl *Control) error {
	if err == nil {
		return nil
	}

	wrapped := classifyRuntimeError(err)
	if persistErr := r.persistFailureState(ctx, state, wrapped); persistErr != nil {
		wrapped = persistErr
	}
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
	if result, resolveErr, handled := r.resolveControl(ctx, state, ctrl); handled {
		if resolveErr != nil {
			return resolveErr
		}
		return runtimeResolvedControlError{result: result}
	}
	return wrapped
}

func (r *DefaultRuntime) persistYield(ctx context.Context, state *RunState, ctrl *Control) (*RunResult, error) {
	state.Status = RunYielded
	state.LastError = ""

	if err := r.saveState(ctx, state, nil); err != nil {
		return nil, err
	}

	return &RunResult{
		RunID:  state.RunID,
		Status: RunYielded,
		Output: "",
		Steps:  state.Step,
		Usage:  state.Usage,
		Yield: &Yield{
			Reason:   ctrl.YieldReason,
			ResumeOK: true,
			Metadata: cloneMetadata(ctrl.YieldMetadata),
		},
	}, nil
}

func (r *DefaultRuntime) persistShortCircuit(ctx context.Context, state *RunState, ctrl *Control) (*RunResult, error) {
	replacement, ok := ctrl.Replacement.(*RunResult)
	if !ok || replacement == nil {
		return nil, r.handleError(ctx, state, "short_circuit", fmt.Errorf("short circuit replacement must be *RunResult, got %T", ctrl.Replacement), ctrl)
	}

	result := *replacement
	if result.RunID == "" {
		result.RunID = state.RunID
	}
	if result.Status == "" {
		result.Status = RunCompleted
	}
	if result.Steps == 0 {
		result.Steps = state.Step
	}
	state.Usage = result.Usage
	state.Step = result.Steps

	var newMessages []Message
	switch result.Status {
	case RunYielded:
		state.Status = RunYielded
		state.LastError = ""
		if result.Yield == nil {
			result.Yield = &Yield{ResumeOK: true}
		}
	case RunFailed:
		state.Status = RunFailed
		state.LastError = result.Output
	case RunCompleted:
		state.Status = RunCompleted
		state.LastError = ""
		preMessageState := cloneRunStateValue(state)
		if result.Output != "" {
			assistantMessage := Message{Role: "assistant", Content: result.Output}
			state.Messages = append(state.Messages, assistantMessage)
			newMessages = []Message{assistantMessage}
		}
		if err := r.saveState(ctx, state, newMessages); err != nil {
			handledErr := r.handleError(ctx, preMessageState, "short_circuit", err, NewControl())
			var resolved runtimeResolvedControlError
			if errors.As(handledErr, &resolved) {
				return resolved.result, nil
			}
			return nil, handledErr
		}
	default:
		state.Status = result.Status
		state.LastError = ""
	}

	if result.Status != RunCompleted {
		if err := r.saveState(ctx, state, newMessages); err != nil {
			return nil, err
		}
	}
	return &result, nil
}

type runtimeResolvedControlError struct {
	result *RunResult
}

func (e runtimeResolvedControlError) Error() string {
	return "runtime control resolved"
}

func (r *DefaultRuntime) persistFailureState(ctx context.Context, state *RunState, err error) error {
	if state == nil || r == nil || r.storage == nil {
		return err
	}
	state.Status = RunFailed
	state.LastError = err.Error()
	if saveErr := r.storage.SaveRun(ctx, state); saveErr != nil {
		if errors.As(err, new(*StorageError)) {
			return err
		}
		return wrapStorageError(saveErr)
	}
	return err
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

func defaultMemory(memory Memory) Memory {
	if memory != nil {
		return memory
	}
	return noopMemory{}
}

func defaultPromptBuilder(builder PromptBuilder) PromptBuilder {
	if builder != nil {
		return builder
	}
	return defaultStaticPromptBuilder{}
}

func defaultContextBuilder(memory Memory) ContextBuilder {
	return defaultContextBuilderImpl{memory: memory}
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
		return true
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

func wrapMemoryError(err error) error {
	if err == nil {
		return nil
	}
	var memoryErr *MemoryError
	if errors.As(err, &memoryErr) {
		return err
	}
	return &MemoryError{Err: err}
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
	case errors.As(err, new(*MemoryError)):
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

func normalizeToolResult(call ToolCall, result *ToolResult) *ToolResult {
	if result == nil {
		result = &ToolResult{}
	}
	cloned := cloneToolResultValue(result)
	if cloned == nil {
		cloned = &ToolResult{}
	}
	if cloned.CallID == "" {
		cloned.CallID = call.CallID
	}
	if cloned.Name == "" {
		cloned.Name = call.Name
	}
	return cloned
}

func toolErrorAsResult(call ToolCall, err error) *ToolResult {
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		err = &ToolError{Err: err}
	}
	return &ToolResult{
		CallID:  call.CallID,
		Name:    call.Name,
		Content: err.Error(),
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

type defaultContextBuilderImpl struct {
	memory Memory
}

func (b defaultContextBuilderImpl) BuildMessages(ctx context.Context, state *RunState) ([]Message, error) {
	return b.memory.BuildContext(ctx, state)
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

func (c runtimeHookChain) runStageWithoutControlStop(call func(Hook) error) error {
	for _, hook := range c.hooks {
		if err := call(hook); err != nil {
			return err
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

func (c runtimeHookChain) BeforeTool(ctx context.Context, event *ToolEvent, ctrl *Control) error {
	return c.runStage(func(h Hook) error { return h.BeforeTool(ctx, event, ctrl) }, ctrl)
}

func (c runtimeHookChain) AfterTool(ctx context.Context, event *ToolEvent, ctrl *Control) error {
	return c.runStageWithoutControlStop(func(h Hook) error { return h.AfterTool(ctx, event, ctrl) })
}

func (c runtimeHookChain) AfterRun(ctx context.Context, event *RunEvent, ctrl *Control) error {
	return c.runStage(func(h Hook) error { return h.AfterRun(ctx, event, ctrl) }, ctrl)
}

func (c runtimeHookChain) OnError(ctx context.Context, event *ErrorEvent, ctrl *Control) error {
	return c.runStage(func(h Hook) error { return h.OnError(ctx, event, ctrl) }, ctrl)
}

func (c runtimeHookChain) OnErrorObserved(ctx context.Context, event *ErrorEvent, ctrl *Control) error {
	return c.runStageWithoutControlStop(func(h Hook) error { return h.OnError(ctx, event, ctrl) })
}

type defaultFileStorage struct {
	root string
}

func newDefaultFileStorage(root string) *defaultFileStorage {
	return &defaultFileStorage{root: root}
}

func (s *defaultFileStorage) LoadRun(_ context.Context, runID string) (*RunState, error) {
	if err := s.validateRunID(runID); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.root, runID, "state.json"))
	if err != nil {
		return nil, s.wrapError("load run snapshot", runID, err)
	}
	var state RunState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, s.wrapError("decode run snapshot", runID, err)
	}
	return &state, nil
}

func (s *defaultFileStorage) LoadMessages(_ context.Context, runID string) ([]Message, error) {
	if err := s.validateRunID(runID); err != nil {
		return nil, err
	}
	file, err := os.Open(filepath.Join(s.root, runID, "messages.jsonl"))
	if err != nil {
		return nil, s.wrapError("load messages log", runID, err)
	}
	defer file.Close()

	messages := make([]Message, 0)
	lineNo := 0
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			lineNo++
			var msg Message
			if trimmed := strings.TrimSpace(string(line)); trimmed != "" {
				if unmarshalErr := json.Unmarshal([]byte(trimmed), &msg); unmarshalErr != nil {
					return nil, s.wrapError("decode messages log", runID, fmt.Errorf("line %d: %w", lineNo, unmarshalErr))
				}
				messages = append(messages, msg)
			}
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			break
		}
		return nil, s.wrapError("read messages log", runID, err)
	}
	return messages, nil
}

func (s *defaultFileStorage) SaveRun(_ context.Context, state *RunState) error {
	if state == nil {
		return s.wrapError("save run snapshot", "", errors.New("run state is nil"))
	}
	if err := s.validateRunID(state.RunID); err != nil {
		return err
	}
	runDir := filepath.Join(s.root, state.RunID)
	if err := s.ensureLayout(state.RunID); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return s.wrapError("encode run snapshot", state.RunID, err)
	}
	if err := s.writeFileAtomically(state.RunID, filepath.Join(runDir, "state.json"), data, 0o644); err != nil {
		return err
	}
	return nil
}

func (s *defaultFileStorage) AppendMessages(_ context.Context, runID string, msgs ...Message) error {
	if err := s.validateRunID(runID); err != nil {
		return err
	}
	if err := s.ensureLayout(runID); err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}
	file, err := os.OpenFile(filepath.Join(s.root, runID, "messages.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return s.wrapError("open messages log", runID, err)
	}

	enc := json.NewEncoder(file)
	for _, msg := range msgs {
		if err := enc.Encode(msg); err != nil {
			_ = file.Close()
			return s.wrapError("append messages log", runID, err)
		}
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return s.wrapError("sync messages log", runID, err)
	}
	if err := file.Close(); err != nil {
		return s.wrapError("close messages log", runID, err)
	}
	return nil
}

func (s *defaultFileStorage) ensureLayout(runID string) error {
	runDir := filepath.Join(s.root, runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return s.wrapError("create run directory", runID, err)
	}
	if err := os.MkdirAll(filepath.Join(runDir, "artifacts"), 0o755); err != nil {
		return s.wrapError("create artifacts directory", runID, err)
	}
	if err := s.ensureJSONFile(runID, filepath.Join(runDir, "meta.json")); err != nil {
		return err
	}
	if err := s.ensureFile(runID, filepath.Join(runDir, "messages.jsonl")); err != nil {
		return err
	}
	return nil
}

func (s *defaultFileStorage) ensureJSONFile(runID, path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return s.wrapError("stat file", runID, err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		return s.wrapError("initialize file", runID, err)
	}
	return nil
}

func (s *defaultFileStorage) ensureFile(runID, path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return s.wrapError("stat file", runID, err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return s.wrapError("initialize file", runID, err)
	}
	return file.Close()
}

func (s *defaultFileStorage) validateRunID(runID string) error {
	if strings.TrimSpace(runID) == "" {
		return s.wrapError("validate run id", runID, errors.New("run id is required"))
	}
	if filepath.IsAbs(runID) || filepath.VolumeName(runID) != "" {
		return s.wrapError("validate run id", runID, errors.New("run id must not be an absolute path"))
	}
	if runID == "." || runID == ".." {
		return s.wrapError("validate run id", runID, errors.New("run id must not reference the current or parent directory"))
	}
	if strings.ContainsAny(runID, `/\`) {
		return s.wrapError("validate run id", runID, errors.New("run id must not contain path separators"))
	}
	if cleaned := filepath.Clean(runID); cleaned != runID || cleaned == "." || cleaned == ".." {
		return s.wrapError("validate run id", runID, errors.New("run id must resolve to a single safe path segment"))
	}
	return nil
}

func (s *defaultFileStorage) writeFileAtomically(runID, path string, data []byte, perm os.FileMode) error {
	tempFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return s.wrapError("create temp run snapshot", runID, err)
	}

	tempPath := tempFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()

	if err := tempFile.Chmod(perm); err != nil {
		_ = tempFile.Close()
		return s.wrapError("set temp run snapshot mode", runID, err)
	}
	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return s.wrapError("write temp run snapshot", runID, err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return s.wrapError("sync temp run snapshot", runID, err)
	}
	if err := tempFile.Close(); err != nil {
		return s.wrapError("close temp run snapshot", runID, err)
	}
	if err := runtimeReplaceFile(tempPath, path); err != nil {
		return s.wrapError("replace run snapshot", runID, err)
	}

	cleanup = false
	return nil
}

func (s *defaultFileStorage) wrapError(action, runID string, err error) error {
	if err == nil {
		return nil
	}
	if runID == "" {
		return &StorageError{Err: fmt.Errorf("%s: %w", action, err)}
	}
	return &StorageError{Err: fmt.Errorf("%s for run %q: %w", action, runID, err)}
}
