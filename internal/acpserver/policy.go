package acpserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

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
	cwd       string
	readOnly  bool
	sequence  uint64
	mu        sync.Mutex
	details   map[acp.ToolCallId]toolDetail
}

type toolDetail struct {
	kind      acp.ToolKind
	locations []acp.ToolCallLocation
	content   []acp.ToolCallContent
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
	locations := toolLocations(h.cwd, call.Arguments)
	content := toolDiff(h.cwd, kind, locations, call.Arguments)
	h.mu.Lock()
	if h.details == nil {
		h.details = make(map[acp.ToolCallId]toolDetail)
	}
	h.details[id] = toolDetail{kind: kind, locations: locations, content: content}
	h.mu.Unlock()
	if err := h.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: h.sessionID,
		Update:    acp.StartToolCall(id, call.Name, acp.WithStartKind(kind), acp.WithStartStatus(acp.ToolCallStatusPending), acp.WithStartLocations(locations), acp.WithStartContent(content)),
	}); err != nil {
		return fmt.Errorf("send tool update: %w", err)
	}
	if h.readOnly && !readTool {
		_ = h.toolStatus(ctx, id, kind, locations, content, acp.ToolCallStatusFailed)
		return ErrReadOnly
	}
	if !readTool {
		decision, err := h.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
			SessionId: h.sessionID,
			ToolCall: acp.ToolCallUpdate{
				ToolCallId: id,
				Title:      acp.Ptr(call.Name),
				Kind:       acp.Ptr(kind),
				Locations:  locations,
				Content:    content,
				Status:     acp.Ptr(acp.ToolCallStatusPending),
			},
			Options: []acp.PermissionOption{
				{Kind: acp.PermissionOptionKindAllowOnce, Name: "Allow once", OptionId: "allow"},
				{Kind: acp.PermissionOptionKindRejectOnce, Name: "Reject", OptionId: "reject"},
			},
		})
		if err != nil {
			_ = h.toolStatus(ctx, id, kind, locations, content, acp.ToolCallStatusFailed)
			return fmt.Errorf("%w: %v", ErrPermissionDenied, err)
		}
		if decision.Outcome.Cancelled != nil {
			return context.Canceled
		}
		if decision.Outcome.Selected == nil || decision.Outcome.Selected.OptionId != "allow" {
			_ = h.toolStatus(ctx, id, kind, locations, content, acp.ToolCallStatusFailed)
			return ErrPermissionDenied
		}
	}
	return h.toolStatus(ctx, id, kind, locations, content, acp.ToolCallStatusInProgress)
}

func (h *permissionHook) AfterTool(ctx context.Context, event *agentcore.ToolEvent, _ *agentcore.Control) error {
	if event == nil || event.Call == nil || h.conn == nil {
		return nil
	}
	id := acp.ToolCallId(event.Call.CallID)
	h.mu.Lock()
	detail, found := h.details[id]
	delete(h.details, id)
	h.mu.Unlock()
	if !found {
		kind, _ := toolKind(event.Call.Name)
		detail = toolDetail{kind: kind, locations: toolLocations(h.cwd, event.Call.Arguments)}
	}
	return h.toolStatus(ctx, id, detail.kind, detail.locations, detail.content, acp.ToolCallStatusCompleted)
}

func (h *permissionHook) toolStatus(ctx context.Context, id acp.ToolCallId, kind acp.ToolKind, locations []acp.ToolCallLocation, content []acp.ToolCallContent, status acp.ToolCallStatus) error {
	return h.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: h.sessionID,
		Update:    acp.UpdateToolCall(id, acp.WithUpdateKind(kind), acp.WithUpdateLocations(locations), acp.WithUpdateContent(content), acp.WithUpdateStatus(status)),
	})
}

// toolLocations resolves path arguments against the ACP workspace and keeps line hints when present.
func toolLocations(cwd string, args map[string]any) []acp.ToolCallLocation {
	if args == nil {
		return nil
	}
	path, _ := args["path"].(string)
	if path == "" {
		path, _ = args["file_path"].(string)
	}
	if path == "" {
		path, _ = args["dir"].(string)
	}
	if path == "" {
		return nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	location := acp.ToolCallLocation{Path: filepath.Clean(path)}
	for _, key := range []string{"line", "line_number"} {
		if line, ok := args[key].(int); ok && line > 0 {
			location.Line = acp.Ptr(line)
			break
		}
		if line, ok := args[key].(float64); ok && line > 0 {
			location.Line = acp.Ptr(int(line))
			break
		}
	}
	if location.Line == nil {
		location.Line = acp.Ptr(1)
	}
	return []acp.ToolCallLocation{location}
}

// toolDiff records the proposed replacement for file_write without reading outside the workspace.
func toolDiff(cwd string, kind acp.ToolKind, locations []acp.ToolCallLocation, args map[string]any) []acp.ToolCallContent {
	if kind != acp.ToolKindEdit || len(locations) == 0 || args == nil {
		return nil
	}
	newText, ok := args["content"].(string)
	if !ok {
		return nil
	}
	rel, err := filepath.Rel(cwd, locations[0].Path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}
	var oldText *string
	if before, err := os.ReadFile(locations[0].Path); err == nil {
		oldText = acp.Ptr(string(before))
	}
	return []acp.ToolCallContent{{Diff: &acp.ToolCallContentDiff{Type: "diff", Path: locations[0].Path, OldText: oldText, NewText: newText}}}
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
	case "web_fetch":
		return acp.ToolKindFetch, true
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
