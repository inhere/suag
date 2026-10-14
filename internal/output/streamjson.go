package output

import (
	"strings"

	"github.com/inhere/suga/internal/personal"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/redact"
)

// SessionLine is the first stream-json line of a run.
func SessionLine(sessionID string) map[string]any {
	return map[string]any{"type": "session", "id": sessionID, "session_id": sessionID}
}

// EventLine maps a runner event to its stream-json line; ok is false for
// events that have no stream-json representation.
func EventLine(event personal.Event) (line map[string]any, ok bool) {
	data := map[string]any{"session_id": event.SessionID, "run_id": event.RunID}
	switch event.Kind {
	case "run_started":
		data["type"] = "turn_start"
	case "run_finished":
		data["type"], data["status"] = "turn_end", event.State
		if event.Usage != nil {
			data["usage"] = personal.ReportRunUsage(event.Usage.Run)
		}
	case "usage":
		// Run-cumulative usage after each model call; the last one equals the
		// turn_end/result "usage" unless tool calls followed it.
		if event.Usage == nil {
			return nil, false
		}
		data["type"], data["usage"] = "usage", personal.ReportRunUsage(event.Usage.Run)
	case string(agentcore.StreamEventTextDelta):
		data["type"], data["text"] = "message_delta", event.Text
	case string(agentcore.StreamEventRetry):
		// restart: the message_delta text since the last message start is
		// void; the message is streamed again from its start.
		data["type"], data["reason"] = "retry", event.Text
		if event.Retry != nil {
			data["attempt"], data["wait_ms"], data["restart"] = event.Retry.Attempt, event.Retry.Wait.Milliseconds(), event.Retry.Restart
		}
	case "plan":
		// Same shape as the ACP plan update: the full list, replaced each time.
		entries := make([]map[string]string, 0, len(event.Plan.Steps))
		for _, step := range event.Plan.Steps {
			entries = append(entries, map[string]string{"step": step.Step, "status": step.Status})
		}
		data["type"], data["explanation"], data["entries"] = "plan", event.Plan.Explanation, entries
	case "tool_call":
		// Text is already the redacted, truncated argument summary (toolEventHook).
		data["type"], data["name"], data["args"] = "tool_call", event.ToolName, event.Text
	case "tool_result":
		data["type"], data["name"], data["is_error"] = "tool_result", event.ToolName, event.IsError
		data["summary"] = ToolResultSummary(event.Text)
	case "agent_progress":
		// A sub-agent's tool call (agent tool); agent is its description.
		data["type"], data["agent"], data["name"], data["args"] = "agent_progress", event.Agent, event.ToolName, event.Text
	default:
		return nil, false
	}
	return data, true
}

// toolSummaryLimit caps the tool_result summary, in runes.
const toolSummaryLimit = 200

// ToolResultSummary condenses a tool result for stream-json. Secrets are
// redacted first and the result truncated afterwards, so a cut can never leave
// a partially redacted credential behind.
func ToolResultSummary(content string) string {
	text := strings.Join(strings.Fields(redact.Secrets(content)), " ")
	if r := []rune(text); len(r) > toolSummaryLimit {
		return string(r[:toolSummaryLimit-1]) + "…"
	}
	return text
}

// ErrorLine is the stream-json line for a failed run.
func ErrorLine(sessionID string, runErr error, result personal.RunResult) map[string]any {
	return WithUsage(map[string]any{"type": "error", "session_id": sessionID, "error": runErr.Error()}, result)
}

// YieldedLine is the input_required line of a run that stopped for more input.
func YieldedLine(sessionID string, result personal.RunResult) map[string]any {
	line := map[string]any{"type": "input_required", "session_id": sessionID, "run_id": result.RunID}
	if len(result.DeniedTools) > 0 {
		line["denied_tools"] = result.DeniedTools
	}
	return WithUsage(line, result)
}

// DeniedLine is the input_required line of a finished turn that had refused tool calls.
func DeniedLine(sessionID string, result personal.RunResult) map[string]any {
	return WithUsage(map[string]any{"type": "input_required", "session_id": sessionID, "run_id": result.RunID, "denied_tools": result.DeniedTools}, result)
}

// ResultLine is the final stream-json line of a successful run. A wrap-up
// answer (result.WrapUp, e.g. "max_steps") carries its reason as "wrap_up", so
// a caller can tell an answer cut short by a limit from a finished one; a
// normal answer leaves the field out.
func ResultLine(sessionID string, result personal.RunResult) map[string]any {
	line := map[string]any{
		"type": "result", "session_id": sessionID, "run_id": result.RunID,
		"result": result.Output, "status": result.State,
	}
	if result.WrapUp != "" {
		line["wrap_up"] = result.WrapUp
	}
	return WithUsage(line, result)
}
