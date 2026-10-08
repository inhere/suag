package acpserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/coder/acp-go-sdk"
	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/platform/tools/native"
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
}

// maxDiffBytes caps each side of a diff; larger files only report locations.
const maxDiffBytes = 256 << 10

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
	// The diff is sent in the initial tool_call and once more in the permission
	// request (so the approver sees it); later status updates carry status only.
	if err := h.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: h.sessionID,
		Update:    acp.StartToolCall(id, call.Name, acp.WithStartKind(kind), acp.WithStartStatus(acp.ToolCallStatusPending), acp.WithStartLocations(locations), acp.WithStartContent(content)),
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

// toolStatus sends a status-only tool_call_update; all other fields are optional
// in ACP and were already delivered by the initial tool_call.
func (h *permissionHook) toolStatus(ctx context.Context, id acp.ToolCallId, status acp.ToolCallStatus) error {
	return h.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: h.sessionID,
		Update:    acp.UpdateToolCall(id, acp.WithUpdateStatus(status)),
	})
}

// toolLocations resolves the path argument exactly like the native file tools
// (native.ResolveSandboxedPath) and keeps a line hint only when one is given.
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
	resolved, err := native.ResolveSandboxedPath(cwd, path)
	if err != nil {
		return nil // the tool will reject this path; nothing to point at
	}
	location := acp.ToolCallLocation{Path: resolved}
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
	return []acp.ToolCallLocation{location}
}

// toolDiff records the proposed replacement for file_write without reading outside the workspace.
// It never blocks: only regular files within maxDiffBytes are read, and binary
// or oversized content yields no diff (locations are still reported).
func toolDiff(cwd string, kind acp.ToolKind, locations []acp.ToolCallLocation, args map[string]any) []acp.ToolCallContent {
	if kind != acp.ToolKindEdit || len(locations) == 0 || args == nil {
		return nil
	}
	newText, ok := args["content"].(string)
	if !ok || len(newText) > maxDiffBytes || !textual(newText) {
		return nil
	}
	rel, err := filepath.Rel(cwd, locations[0].Path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}
	root, err := os.OpenRoot(cwd)
	if err != nil {
		return nil
	}
	defer root.Close()

	var oldText *string
	info, err := root.Lstat(rel)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// Absent target (including a missing parent directory, which file_write
		// creates) is a new file. os.Root already rejects ".." and symlink
		// escapes with a non-NotExist error, which falls to the nil branch.
	case err != nil:
		return nil
	default:
		if info.Mode()&os.ModeSymlink != 0 {
			// Follow only symlinks that stay inside the root; dangling ones fail closed.
			if info, err = root.Stat(rel); err != nil {
				return nil
			}
		}
		// Opening a FIFO/device would block before the user can even decide.
		if !info.Mode().IsRegular() || info.Size() > maxDiffBytes {
			return nil
		}
		before, ok := readBounded(root, rel)
		if !ok {
			return nil
		}
		oldText = acp.Ptr(before)
	}
	return []acp.ToolCallContent{{Diff: &acp.ToolCallContentDiff{Type: "diff", Path: locations[0].Path, OldText: oldText, NewText: newText}}}
}

// readBounded reads a regular file, giving up if it grew past maxDiffBytes or is binary.
func readBounded(root *os.Root, rel string) (string, bool) {
	f, err := root.Open(rel)
	if err != nil {
		return "", false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxDiffBytes+1))
	if err != nil || len(data) > maxDiffBytes || !textual(string(data)) {
		return "", false
	}
	return string(data), true
}

// textual reports whether s is valid UTF-8 without NUL bytes (a binary marker).
func textual(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
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
