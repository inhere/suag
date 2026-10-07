package acpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/coder/acp-go-sdk"
	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/pkg/agentcore"
)

var ErrPermissionDenied = errors.New("ACP tool permission denied")
var ErrReadOnly = errors.New("tool is unavailable in read_only mode")

type permissionHook struct {
	agentcore.NopHook
	conn      *acp.AgentSideConnection
	sessionID acp.SessionId
	readOnly  bool
	sequence  uint64
}

func (h *permissionHook) BeforeTool(ctx context.Context, event *agentcore.ToolEvent, _ *agentcore.Control) error {
	if event == nil || event.Call == nil || h.conn == nil {
		return ErrPermissionDenied
	}
	call := event.Call
	if call.CallID == "" {
		h.sequence++
		call.CallID = fmt.Sprintf("%s-%d", event.RunID, h.sequence)
	}
	id := acp.ToolCallId(call.CallID)
	kind, readTool := toolKind(call.Name)
	if err := h.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: h.sessionID,
		Update:    acp.StartToolCall(id, call.Name, acp.WithStartKind(kind), acp.WithStartStatus(acp.ToolCallStatusPending)),
	}); err != nil {
		return fmt.Errorf("send tool update: %w", err)
	}
	if h.readOnly && !readTool {
		_ = h.toolStatus(ctx, id, acp.ToolCallStatusFailed)
		return ErrReadOnly
	}
	if !readTool {
		decision, err := h.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
			SessionId: h.sessionID,
			ToolCall: acp.ToolCallUpdate{
				ToolCallId: id,
				Title:      acp.Ptr(call.Name),
				Kind:       acp.Ptr(kind),
				Status:     acp.Ptr(acp.ToolCallStatusPending),
			},
			Options: []acp.PermissionOption{
				{Kind: acp.PermissionOptionKindAllowOnce, Name: "Allow once", OptionId: "allow"},
				{Kind: acp.PermissionOptionKindRejectOnce, Name: "Reject", OptionId: "reject"},
			},
		})
		if err != nil {
			_ = h.toolStatus(ctx, id, acp.ToolCallStatusFailed)
			return fmt.Errorf("%w: %v", ErrPermissionDenied, err)
		}
		if decision.Outcome.Cancelled != nil {
			return context.Canceled
		}
		if decision.Outcome.Selected == nil || decision.Outcome.Selected.OptionId != "allow" {
			_ = h.toolStatus(ctx, id, acp.ToolCallStatusFailed)
			return ErrPermissionDenied
		}
	}
	return h.toolStatus(ctx, id, acp.ToolCallStatusInProgress)
}

func (h *permissionHook) AfterTool(ctx context.Context, event *agentcore.ToolEvent, _ *agentcore.Control) error {
	if event == nil || event.Call == nil || h.conn == nil {
		return nil
	}
	return h.toolStatus(ctx, acp.ToolCallId(event.Call.CallID), acp.ToolCallStatusCompleted)
}

func (h *permissionHook) toolStatus(ctx context.Context, id acp.ToolCallId, status acp.ToolCallStatus) error {
	return h.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: h.sessionID,
		Update:    acp.UpdateToolCall(id, acp.WithUpdateStatus(status)),
	})
}

func toolKind(name string) (acp.ToolKind, bool) {
	switch name {
	case "file_read", "file_list":
		return acp.ToolKindRead, true
	case "file_write":
		return acp.ToolKindEdit, false
	case "shell_exec":
		return acp.ToolKindExecute, false
	case "web_search":
		return acp.ToolKindSearch, true
	default:
		return acp.ToolKindOther, false // Unknown MCP tools require permission.
	}
}

func mcpSettings(servers []acp.McpServer) ([]appconfig.MCPServerSettings, error) {
	settings := make([]appconfig.MCPServerSettings, 0, len(servers))
	seen := make(map[string]bool)
	for _, server := range servers {
		if server.Stdio == nil {
			return nil, acp.NewInvalidParams(map[string]any{"mcpServers": "only stdio transport is supported"})
		}
		stdio := server.Stdio
		name, command := strings.TrimSpace(stdio.Name), strings.TrimSpace(stdio.Command)
		if name == "" || command == "" || seen[name] {
			return nil, acp.NewInvalidParams(map[string]any{"mcpServers": "server needs unique name and command"})
		}
		seen[name] = true
		env := make(map[string]string, len(stdio.Env))
		for _, variable := range stdio.Env {
			if strings.TrimSpace(variable.Name) == "" {
				return nil, acp.NewInvalidParams(map[string]any{"mcpServers": "environment variable name is required"})
			}
			env[variable.Name] = variable.Value
		}
		settings = append(settings, appconfig.MCPServerSettings{ID: name, Command: command, Args: append([]string(nil), stdio.Args...), Env: env})
	}
	return settings, nil
}
