package acpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/coder/acp-go-sdk"
	"github.com/inhere/suag/pkg/agentcore/memory"
	"github.com/inhere/suag/pkg/agentcore/redact"
	"github.com/inhere/suag/pkg/agentcore/tools/native"
)

// ErrMemoryWriteRejected is the refusal of a memory write confirmation.
var ErrMemoryWriteRejected = errors.New("用户拒绝写入记忆")

// memoryBodyPreview bounds the body shown in a confirmation, in runes.
const memoryBodyPreview = 300

// ConfirmMemory implements TurnGate: a memory write from a tainted session is
// confirmed through session/request_permission on the memory tool call.
// Allowed → nil; rejected or a failed request → ErrMemoryWriteRejected;
// cancelled (outcome or turn) → context.Canceled, which ends the turn.
func (h *permissionHook) ConfirmMemory(ctx context.Context, s memory.Scope, op memory.Op) error {
	if h.conn == nil {
		return ErrMemoryWriteRejected
	}
	id := native.MemoryCallID(ctx)
	if id == "" {
		h.sequence++
		id = fmt.Sprintf("memory-confirm-%d", h.sequence)
	}
	decision, err := h.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
		SessionId: h.sessionID,
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: acp.ToolCallId(id),
			Title:      acp.Ptr("memory: 会话读过外部内容，确认写入记忆"),
			Kind:       acp.Ptr(acp.ToolKindEdit),
			Content:    []acp.ToolCallContent{acp.ToolContent(acp.TextBlock(memoryConfirmText(s, op)))},
		},
		Options: []acp.PermissionOption{
			{Kind: acp.PermissionOptionKindAllowOnce, Name: "Allow once", OptionId: "allow"},
			{Kind: acp.PermissionOptionKindRejectOnce, Name: "Reject", OptionId: "reject"},
		},
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("%w: %v", ErrMemoryWriteRejected, err)
	}
	if decision.Outcome.Cancelled != nil {
		return context.Canceled
	}
	if decision.Outcome.Selected == nil || decision.Outcome.Selected.OptionId != "allow" {
		return ErrMemoryWriteRejected
	}
	return nil
}

// memoryConfirmText describes the write. Confirm receives the op before the
// governance layer sanitizes it, so every field is cleaned here first.
func memoryConfirmText(s memory.Scope, op memory.Op) string {
	lines := []string{"范围：" + s.String(), "操作：" + string(op.Action)}
	if op.Kind != "" {
		lines = append(lines, "类型："+string(op.Kind))
	}
	if op.ID != "" {
		lines = append(lines, "id："+op.ID)
	}
	if op.Title != "" {
		lines = append(lines, "标题："+redact.Clean(op.Title))
	}
	if op.Body != "" {
		body := []rune(redact.Clean(op.Body))
		if len(body) > memoryBodyPreview {
			body = append(body[:memoryBodyPreview], '…')
		}
		lines = append(lines, "正文："+string(body))
	}
	return strings.Join(lines, "\n")
}
