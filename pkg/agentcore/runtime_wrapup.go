package agentcore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// How a run ends: finishing it (finishRun and the error path that returns
// through it), persisting the final answer, and the wrap-up answer of a step
// limit or a hook's Control.RequestWrapUp. The main loop stays in runtime.go.

// wrapUpReasonMaxSteps is the wrap-up reason the runtime itself reports when a
// step limit asked for the final answer; a hook's Control.RequestWrapUp brings
// its own reason (a token budget, a timeout).
const wrapUpReasonMaxSteps = "max_steps"

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

func (r *DefaultRuntime) persistFinalAnswer(ctx context.Context, input RunInput, state *RunState, result *GenerateResult) (*RunResult, error) {
	preOutputState := cloneRunStateValue(state)
	assistantMessage := Message{
		Role:      "assistant",
		Content:   result.Content,
		Metadata:  cloneMetadata(result.Metadata),
		Reasoning: result.Reasoning,
	}
	state.Messages = append(state.Messages, assistantMessage)
	if len(result.Artifacts) > 0 {
		state.Artifacts = append(state.Artifacts, result.Artifacts...)
	}
	state.Status = RunCompleted
	state.LastError = ""

	if err := r.saveState(ctx, state, []Message{assistantMessage}); err != nil {
		return r.returnWithHandledError(ctx, input, stateForSaveStateFailure(preOutputState, state, err), "persist_output", err, NewControl())
	}

	return &RunResult{
		RunID:  state.RunID,
		Status: RunCompleted,
		Output: result.Content,
		Steps:  state.Step,
		Usage:  state.Usage,
	}, nil
}

// finishIgnoredWrapUp ends a wrap-up step whose reply asked for tools again
// instead of answering, so the turn reports what it has instead of failing with
// everything done before the limit lost. In order:
//
//  1. Text sent alongside the tool calls is the answer; the calls do not run.
//  2. Without text, the same request goes out again asking for a reply without
//     tools (GenerateRequest.ToolChoice), when the provider takes one. Its
//     usage counts, but it is not a step, so the limit is not checked again.
//  3. Still without text, a fixed notice naming why the turn ended answers,
//     repeating the run's last text so the work is not lost.
//
// The run completes as an ordinary wrap-up (WrapUp keeps the reason: the step
// limit or what a hook requested), which hosts present as a completed run.
func (r *DefaultRuntime) finishIgnoredWrapUp(ctx context.Context, input RunInput, state *RunState, req *GenerateRequest, res *GenerateResult, wrapUp string, maxSteps int, ctrl *Control) (*RunResult, error) {
	if strings.TrimSpace(res.Content) != "" {
		return r.finishWrapUpAnswer(ctx, input, state, wrapUpAnswerOf(res), wrapUp)
	}

	if r.supportsToolChoice() {
		// Same messages, same wrap-up instruction: only tool use is forbidden.
		retryReq := *req
		retryReq.ToolChoice = ToolChoiceNone
		retryRes, err := r.callModel(ctx, state, &retryReq, ctrl, input.Stream)
		if err != nil {
			return r.returnWithHandledError(ctx, input, state, "model", err, ctrl)
		}
		if retryRes == nil {
			if result, err, handled := r.resolveControl(ctx, state, ctrl); handled {
				return r.finishRun(ctx, input, state, result, err)
			}
			return r.returnWithHandledError(ctx, input, state, "model", fmt.Errorf("model execution returned no result"), ctrl)
		}
		state.Usage = state.Usage.Add(retryRes.Usage)
		if result, err, handled := r.resolveControl(ctx, state, ctrl); handled {
			return r.finishRun(ctx, input, state, result, err)
		}
		if strings.TrimSpace(retryRes.Content) != "" {
			// Any tool calls of the retry are ignored like the first reply's.
			return r.finishWrapUpAnswer(ctx, input, state, wrapUpAnswerOf(retryRes), wrapUp)
		}
	}

	return r.finishWrapUpAnswer(ctx, input, state, &GenerateResult{
		Content:      wrapUpFallbackNotice(wrapUp, maxSteps, state),
		FinishReason: FinishFinal,
	}, wrapUp)
}

// wrapUpAnswerOf turns a reply into the answer its turn ends with. A reply that
// asked for tools keeps its text only: the calls do not run, so its turn
// metadata (signed thinking blocks, reasoning items) belongs to them and is not
// replayed later.
func wrapUpAnswerOf(res *GenerateResult) *GenerateResult {
	answer := &GenerateResult{
		Content:      res.Content,
		Reasoning:    res.Reasoning,
		FinishReason: FinishFinal,
	}
	if len(res.ToolCalls) == 0 {
		answer.Metadata = cloneMetadata(res.Metadata)
		answer.Artifacts = res.Artifacts
	}
	return answer
}

// finishWrapUpAnswer persists answer as the run's final answer and completes the
// run, keeping the wrap-up reason on the result (what the FinishFinal branch of
// Run does for a model that complied).
func (r *DefaultRuntime) finishWrapUpAnswer(ctx context.Context, input RunInput, state *RunState, answer *GenerateResult, wrapUp string) (*RunResult, error) {
	result, err := r.persistFinalAnswer(ctx, input, state, answer)
	if err != nil {
		return nil, err
	}
	if result != nil {
		result.WrapUp = wrapUp
	}
	return r.finishRun(ctx, input, state, result, nil)
}

// supportsToolChoice reports whether the provider honours a request that asks
// for a reply without tools (the wrap-up retry): a provider that does not
// declare the capability is never asked.
func (r *DefaultRuntime) supportsToolChoice() bool {
	chooser, ok := r.provider.(ToolChooser)
	return ok && chooser.SupportsToolChoice()
}

// wrapUpFallbackNotice is the fixed answer of a wrap-up step whose model never
// sent any text, not even when asked again without tools: it names why the turn
// ended and repeats the run's last text, so a short answer is not mistaken for
// a finished task and the work before the limit is not lost.
func wrapUpFallbackNotice(reason string, maxSteps int, state *RunState) string {
	var why string
	switch reason {
	case wrapUpReasonMaxSteps:
		if maxSteps > 0 {
			why = fmt.Sprintf("本轮因步数上限（%d 步）收尾", maxSteps)
			break
		}
		why = "本轮因步数上限收尾"
	case "":
		why = "本轮收尾"
	default:
		why = fmt.Sprintf("本轮因 %s 收尾", reason)
	}
	notice := why + "，模型未给出汇报。"
	if last := lastAssistantText(state); last != "" {
		notice += "\n\n最后一次有正文的回复如下：\n\n" + last
	}
	return notice
}

// lastAssistantText is the text of the run's last assistant message that has
// any ("" when none did): the model's last words besides its tool calls, which
// the wrap-up fallback repeats.
func lastAssistantText(state *RunState) string {
	if state == nil {
		return ""
	}
	for i := len(state.Messages) - 1; i >= 0; i-- {
		message := state.Messages[i]
		if message.Role == "assistant" && strings.TrimSpace(message.Content) != "" {
			return message.Content
		}
	}
	return ""
}
