package agentcore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/inhere/axon/pkg/agentcore/files"
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
		return r.returnWithHandledError(ctx, input, stateForSaveStateFailure(preInputState, state, err), "persist_input", err, ctrl)
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

		res, err := r.callModel(ctx, state, req, ctrl, input.Stream)
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
	usingDefaultMemoryContext := false
	if builder == nil {
		builder = defaultContextBuilder(r.memory)
		usingDefaultMemoryContext = true
	}
	messages, err := builder.BuildMessages(ctx, state)
	if err != nil {
		if usingDefaultMemoryContext {
			return nil, wrapMemoryError(err)
		}
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

func (r *DefaultRuntime) callModel(ctx context.Context, state *RunState, req *GenerateRequest, ctrl *Control, stream StreamFunc) (*GenerateResult, error) {
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

	result, err := r.invokeProvider(ctx, event.Request, stream)
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

func (r *DefaultRuntime) invokeProvider(ctx context.Context, req *GenerateRequest, stream StreamFunc) (*GenerateResult, error) {
	if stream != nil {
		if provider, ok := r.provider.(StreamingProvider); ok {
			return provider.Stream(ctx, req, func(ctx context.Context, event StreamEvent) error {
				return stream(ctx, cloneStreamEventValue(event))
			})
		}
	}

	return r.provider.Generate(ctx, req)
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

func stateForSaveStateFailure(preState *RunState, currentState *RunState, err error) *RunState {
	var memoryErr *MemoryError
	if errors.As(err, &memoryErr) {
		return currentState
	}
	return preState
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
		return r.returnWithHandledError(ctx, input, stateForSaveStateFailure(preOutputState, state, err), "persist_output", err, NewControl())
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
			handledErr := r.handleError(ctx, stateForSaveStateFailure(preMessageState, state, err), "short_circuit", err, NewControl())
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
	return files.NewStorage(filepath.Join(os.TempDir(), "agentcore-runs"))
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

func cloneStreamEventValue(event StreamEvent) StreamEvent {
	return StreamEvent{
		Kind:     event.Kind,
		Delta:    event.Delta,
		Metadata: cloneMetadata(event.Metadata),
	}
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
