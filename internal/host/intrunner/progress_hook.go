package intrunner

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/inhere/axon/internal/host/session"
	"github.com/inhere/axon/pkg/agentcore"
)

type hostProgressEmitter struct {
	mu        sync.Mutex
	emit      func(session.RuntimeEvent) error
	nextSeq   int
	messageID string
}

func (e *hostProgressEmitter) Set(emit func(session.RuntimeEvent) error) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.emit = emit
	e.nextSeq = 0
	e.messageID = ""
}

func (e *hostProgressEmitter) Emit(event session.RuntimeEvent) error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	emit := e.emit
	e.mu.Unlock()
	if emit == nil || strings.TrimSpace(event.EventType) == "" {
		return nil
	}
	return emit(event)
}

func (e *hostProgressEmitter) NextMessageID(runID string) string {
	if e == nil {
		return strings.TrimSpace(runID) + ":assistant"
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.messageID == "" {
		e.messageID = strings.TrimSpace(runID) + ":assistant"
	}
	return e.messageID
}

func (e *hostProgressEmitter) NextActivityID(runID, prefix string) string {
	if e == nil {
		return strings.TrimSpace(runID) + ":" + prefix
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.nextSeq++
	return strings.TrimSpace(runID) + ":" + prefix + ":" + strconv.Itoa(e.nextSeq)
}

func withHostProgressHook(hooks []agentcore.Hook, emitter *hostProgressEmitter) []agentcore.Hook {
	out := make([]agentcore.Hook, 0, len(hooks)+1)
	out = append(out, newHostProgressHook(emitter))
	out = append(out, hooks...)
	return out
}

type hostProgressHook struct {
	agentcore.NopHook
	emitter *hostProgressEmitter
}

func newHostProgressHook(emitter *hostProgressEmitter) agentcore.Hook {
	return &hostProgressHook{emitter: emitter}
}

func (h *hostProgressHook) BeforeModel(_ context.Context, event *agentcore.ModelEvent, _ *agentcore.Control) error {
	if event == nil {
		return nil
	}
	return h.emitActivity(event.RunID, "model", "running", "LLM request", modelRequestDetail(event.Request), modelRequestPayload(event.Request))
}

func (h *hostProgressHook) AfterModel(_ context.Context, event *agentcore.ModelEvent, _ *agentcore.Control) error {
	if event == nil {
		return nil
	}
	title := "LLM completed"
	if event.Result != nil && len(event.Result.ToolCalls) > 0 {
		title = "LLM returned tool calls"
	}
	return h.emitActivity(event.RunID, "model", "completed", title, modelResultDetail(event.Result), modelResultPayload(event.Result))
}

func (h *hostProgressHook) BeforeTool(_ context.Context, event *agentcore.ToolEvent, _ *agentcore.Control) error {
	if event == nil || event.Call == nil {
		return nil
	}
	return h.emitActivity(event.RunID, "tool", "running", "Tool "+event.Call.Name, toolCallDetail(event.Call), toolCallPayload(event.Call))
}

func (h *hostProgressHook) AfterTool(_ context.Context, event *agentcore.ToolEvent, _ *agentcore.Control) error {
	if event == nil || event.Call == nil {
		return nil
	}
	status := "completed"
	if event.Result != nil && strings.HasPrefix(strings.TrimSpace(event.Result.Content), "tool error:") {
		status = "failed"
	}
	return h.emitActivity(event.RunID, "tool", status, "Tool "+event.Call.Name, toolResultDetail(event.Result), toolResultPayload(event.Result))
}

func (h *hostProgressHook) emitActivity(runID, kind, status, title, detail string, details map[string]any) error {
	if h == nil || h.emitter == nil {
		return nil
	}
	activityID := h.emitter.NextActivityID(runID, kind)
	payload := map[string]any{
		"activityId": activityID,
		"kind":       kind,
		"status":     status,
		"title":      title,
		"detail":     detail,
	}
	for key, value := range details {
		payload[key] = value
	}
	return h.emitter.Emit(session.RuntimeEvent{
		EventType: session.EventRunExternalActivity,
		Payload:   payload,
	})
}

func modelRequestDetail(req *agentcore.GenerateRequest) string {
	if req == nil {
		return "preparing model request"
	}
	return fmt.Sprintf("messages=%d tools=%d", len(req.Messages), len(req.Tools))
}

func modelRequestPayload(req *agentcore.GenerateRequest) map[string]any {
	if req == nil {
		return map[string]any{}
	}
	return map[string]any{
		"model":        strings.TrimSpace(req.Model),
		"messageCount": len(req.Messages),
		"toolCount":    len(req.Tools),
		"toolNames":    toolSchemaNames(req.Tools),
		"messages":     messageSummaries(req.Messages, 8),
	}
}

func modelResultDetail(result *agentcore.GenerateResult) string {
	if result == nil {
		return "model returned no result"
	}
	if len(result.ToolCalls) > 0 {
		names := make([]string, 0, len(result.ToolCalls))
		for _, call := range result.ToolCalls {
			if strings.TrimSpace(call.Name) != "" {
				names = append(names, call.Name)
			}
		}
		if len(names) > 0 {
			return "tools=" + strings.Join(names, ", ")
		}
		return fmt.Sprintf("tool_calls=%d", len(result.ToolCalls))
	}
	if strings.TrimSpace(result.Content) != "" {
		return "content ready"
	}
	return string(result.FinishReason)
}

func modelResultPayload(result *agentcore.GenerateResult) map[string]any {
	if result == nil {
		return map[string]any{}
	}
	return map[string]any{
		"finishReason":   string(result.FinishReason),
		"contentPreview": truncateText(result.Content, 800),
		"contentLength":  len(result.Content),
		"toolCallCount":  len(result.ToolCalls),
		"toolCalls":      toolCallSummaries(result.ToolCalls),
		"usage": map[string]any{
			"inputTokens":  result.Usage.InputTokens,
			"outputTokens": result.Usage.OutputTokens,
			"totalTokens":  result.Usage.TotalTokens,
		},
	}
}

func toolCallDetail(call *agentcore.ToolCall) string {
	if call == nil {
		return "executing tool"
	}
	return fmt.Sprintf("args=%d", len(call.Arguments))
}

func toolCallPayload(call *agentcore.ToolCall) map[string]any {
	if call == nil {
		return map[string]any{}
	}
	return map[string]any{
		"callId":    strings.TrimSpace(call.CallID),
		"toolName":  strings.TrimSpace(call.Name),
		"arguments": cloneAnyMap(call.Arguments),
	}
}

func toolResultDetail(result *agentcore.ToolResult) string {
	if result == nil {
		return "tool returned no result"
	}
	content := strings.TrimSpace(result.Content)
	if content == "" {
		return "tool completed"
	}
	if len(content) > 160 {
		return content[:157] + "..."
	}
	return content
}

func toolResultPayload(result *agentcore.ToolResult) map[string]any {
	if result == nil {
		return map[string]any{}
	}
	return map[string]any{
		"callId":         strings.TrimSpace(result.CallID),
		"toolName":       strings.TrimSpace(result.Name),
		"contentPreview": truncateText(result.Content, 1200),
		"contentLength":  len(result.Content),
	}
}

func toolSchemaNames(tools []agentcore.ToolSchema) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func messageSummaries(messages []agentcore.Message, limit int) []map[string]any {
	if limit <= 0 || len(messages) == 0 {
		return nil
	}
	start := 0
	if len(messages) > limit {
		start = len(messages) - limit
	}
	out := make([]map[string]any, 0, len(messages)-start)
	for _, message := range messages[start:] {
		out = append(out, map[string]any{
			"role":           strings.TrimSpace(message.Role),
			"contentPreview": truncateText(message.Content, 500),
			"contentLength":  len(message.Content),
		})
	}
	return out
}

func toolCallSummaries(calls []agentcore.ToolCall) []map[string]any {
	out := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		out = append(out, map[string]any{
			"callId":    strings.TrimSpace(call.CallID),
			"toolName":  strings.TrimSpace(call.Name),
			"arguments": cloneAnyMap(call.Arguments),
		})
	}
	return out
}

func cloneAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func truncateText(text string, limit int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if limit <= 0 || len(runes) <= limit {
		return text
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}
