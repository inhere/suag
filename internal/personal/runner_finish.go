package personal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore"
)

// Ending a turn: the last steps of Runner.Run after the runtime returned
// (appendTurnOutput) and the finish function that summarizes a turn - status,
// usage, the turn and run_finished events, back to open with the session
// usage. turnInput is the input marker finish reads back from the context.

// appendTurnOutput replays the turn's tool results (with their call arguments)
// and the final answer into the session transcript.
func appendTurnOutput(ctx context.Context, transcript *Transcript, storage agentcore.Storage, sessionID, runID string, coreResult *agentcore.RunResult) {
	if messages, err := storage.LoadMessages(ctx, runID); err != nil {
		slog.Warn("read personal run transcript", "session_id", sessionID, "err", err)
	} else {
		// Tool arguments live on the assistant's ToolCall message; index them by call id
		// so each result can be summarized with its call parameters.
		calls := map[string]map[string]any{}
		for _, message := range messages {
			if message.ToolCall != nil {
				calls[message.ToolCall.CallID] = message.ToolCall.Arguments
			}
		}
		for _, message := range messages {
			if message.ToolResult == nil {
				continue
			}
			if err := transcript.AppendToolCall(message.ToolResult.Name, calls[message.ToolResult.CallID], message.ToolResult.Content); err != nil {
				slog.Warn("append personal transcript", "session_id", sessionID, "err", err)
			}
		}
	}
	if coreResult != nil {
		if err := transcript.AppendAssistant(coreResult.Output); err != nil {
			slog.Warn("append personal transcript", "session_id", sessionID, "err", err)
		}
	}
}

// withSessionUsage fills result.SessionUsage with the session-cumulative usage
// (model events of the session and of its sub-agent sessions). Error returns that happen before finish use it
// too, so callers such as the ACP usage_update keep the previously known total
// instead of seeing a zero value. A lookup failure leaves the value untouched.
func (r *Runner) withSessionUsage(ctx context.Context, sessionID string, result RunResult) RunResult {
	// The caller's ctx may already be cancelled or timed out (lock wait).
	sum, err := r.cfg.Repository.SumUsageTree(context.WithoutCancel(ctx), sessionID)
	if err != nil {
		slog.Warn("sum session usage", "session_id", sessionID, "err", err)
		return result
	}
	result.SessionUsage = usageFromSum(sum)
	return result
}

func (r *Runner) finish(ctx context.Context, model Session, result RunResult, turnStart time.Time, usage *usageCollector, coreResult *agentcore.RunResult, runErr error, sink EmitFunc) (RunResult, error) {
	if coreResult != nil {
		result.Status, result.Output, result.Usage = coreResult.Status, coreResult.Output, coreResult.Usage
		result.WrapUp = coreResult.WrapUp
	}
	// Prefer the hook-collected sum: it also covers failed/cancelled turns.
	turnUsage, calls, lastInput := usage.snapshot()
	result.ModelCalls, result.LastInputTokens = calls, lastInput
	if calls > 0 {
		result.Usage = turnUsage
	}
	if coreResult == nil && runErr == nil {
		runErr = fmt.Errorf("runtime returned no result")
	}
	if runErr != nil {
		result.Status = agentcore.RunFailed
	}
	// An aborted approval (cancelled permission prompt) cancels the turn even
	// when the turn context itself is still live.
	canceled := ctx.Err() != nil || errors.Is(runErr, context.Canceled)
	if canceled {
		result.Status = agentcore.RunFailed
	}
	denials := turnDenialsFrom(ctx)
	result.DeniedTools, result.DenialLimitReached = denials.tools(), denials.limitReached()

	// A yielded turn is stored as completed (its messages become history for the
	// next turn) but reported to callers as INPUT_REQUIRED.
	runState, turnStatus, stopReason := RunCompleted, store.TurnCompleted, ""
	switch result.Status {
	case agentcore.RunCompleted:
	case agentcore.RunYielded:
		runState, stopReason = RunInputRequired, "input_required"
	case agentcore.RunFailed:
		if canceled {
			runState, turnStatus, stopReason = RunCanceled, store.TurnCancelled, "cancelled"
		} else {
			runState, turnStatus, stopReason = RunFailed, store.TurnFailed, "run_failed"
		}
	default:
		runState, turnStatus, stopReason = RunFailed, store.TurnFailed, "unexpected_status"
	}
	result.State = runState
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	now := time.Now().UTC()
	var persistErr error
	if err := r.cfg.Repository.UpdateTurn(finishCtx, store.Turn{ID: result.TurnID, Status: turnStatus, StopReason: stopReason, EndedAt: now}); err != nil {
		persistErr = errors.Join(persistErr, err)
	}
	// The turn event is written outside the entry transactions and is
	// best-effort; it is also what the trace timeline anchors on.
	turnAttrs := map[string]any{"stop_reason": stopReason, "status": string(result.Status)}
	// What started the turn: the input's length and whether a person typed
	// it (injected = a hook reply, gofer delivery, ...).
	if in, ok := ctx.Value(turnInputKey{}).(turnInput); ok {
		turnAttrs["prompt_chars"] = in.chars
		if in.injected {
			turnAttrs["injected"] = true
		}
	}
	if n := denials.count(); n > 0 {
		turnAttrs["denials"] = n
		turnAttrs["denial_limit_reached"] = result.DenialLimitReached
	}
	r.rec.record(finishCtx, store.Event{
		SessionID: model.ID, TurnID: result.TurnID, Kind: store.EventTurn, Name: "turn",
		Status: strings.ToLower(string(runState)), StartedAt: turnStart, DurationMs: time.Since(turnStart).Milliseconds(),
		Attrs: turnAttrs,
	})
	// Session-cumulative usage is derived from the model events of the session,
	// which already include this turn's calls (written synchronously above).
	result = r.withSessionUsage(finishCtx, model.ID, result)
	result.RunUsage = runUsageFrom(ctx).snapshot()
	// Back to open with the current metadata (status columns only).
	if fresh, err := r.cfg.Repository.GetSession(finishCtx, model.ID); err != nil {
		persistErr = errors.Join(persistErr, err)
	} else {
		fresh.Status, fresh.UpdatedAt = store.SessionOpen, now
		if err := r.cfg.Repository.UpdateSessionFields(finishCtx, fresh); err != nil {
			persistErr = errors.Join(persistErr, err)
		}
	}
	final := &UsageUpdate{Run: result.RunUsage, Session: result.SessionUsage}
	if err := emit(finishCtx, sink, Event{Kind: "run_finished", SessionID: result.SessionID, TurnID: result.TurnID, RunID: result.RunID, Status: result.Status, State: result.State, Text: result.Output, Usage: final}); err != nil {
		persistErr = errors.Join(persistErr, err)
	}
	return result, errors.Join(runErr, persistErr)
}

// turnInput is the turn's input as seen by finish (the turn event).
type turnInput struct {
	chars    int
	injected bool
}

type turnInputKey struct{}
