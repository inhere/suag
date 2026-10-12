package acpserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/coder/acp-go-sdk"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/tools"
	"github.com/inhere/suag/pkg/agentcore/tools/mcp"
	"github.com/inhere/suag/pkg/agentcore/tools/native"
)

var ErrPermissionDenied = errors.New("ACP tool permission denied")
var ErrReadOnly = errors.New("tool is unavailable in read_only mode")

// deniedError refuses a tool call: it matches tools.ErrDenied (so the approval
// layer returns it to the model as a tool error and observe records a denial)
// and keeps the ACP cause (ErrPermissionDenied / ErrReadOnly) reachable.
type deniedError struct{ cause error }

func (e *deniedError) Error() string   { return e.cause.Error() }
func (e *deniedError) Unwrap() []error { return []error{tools.ErrDenied, e.cause} }

// callState tracks one tool call between BeforeTool, Decide and AfterTool.
type callState int

const (
	callPending    callState = iota // tool_call sent, not yet decided
	callInProgress                  // approved, in_progress sent
	callDenied                      // refused, failed sent
)

// pendingCall is what BeforeTool computed for the notification; Decide reuses
// it for the permission request so the diff is read once.
type pendingCall struct {
	state     callState
	kind      acp.ToolKind
	readTool  bool
	locations []acp.ToolCallLocation
	content   []acp.ToolCallContent
}

// permissionHook is the per-prompt ACP turn gate. As an agentcore.Hook it sends
// the tool_call notifications (pending, in_progress, completed/failed); as a
// tools.Approver (Decide) it is the approval layer's policy: read_only
// refusals and RequestPermission for MCP/write tools.
type permissionHook struct {
	agentcore.NopHook
	conn      *acp.AgentSideConnection
	sessionID acp.SessionId
	cwd       string
	readOnly  bool
	sequence  uint64
	// lookup resolves a tool name in this runtime (set after it opens); the
	// announced kind and the read-only fast path come from the tool's
	// tools.Meta, so an MCP tool called "web_search" cannot pass for the
	// built-in read tool.
	lookup func(name string) agentcore.Tool

	mu    sync.Mutex
	calls map[acp.ToolCallId]*pendingCall
}

var _ tools.Approver = (*permissionHook)(nil)

// maxDiffBytes caps each side of a diff; larger files only report locations.
const maxDiffBytes = 256 << 10

// BeforeTool announces the call (status pending, with locations and diff). The
// permission decision happens later in Decide, inside the approval layer.
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
	kind, readTool := toolKind(h.tool(call.Name))
	locations := toolLocations(h.cwd, call.Arguments)
	content := toolDiff(h.cwd, kind, locations, call.Arguments)
	h.mu.Lock()
	if h.calls == nil {
		h.calls = map[acp.ToolCallId]*pendingCall{}
	}
	h.calls[id] = &pendingCall{kind: kind, readTool: readTool, locations: locations, content: content}
	h.mu.Unlock()
	// The diff is sent in the initial tool_call and once more in the permission
	// request (so the approver sees it); later status updates carry status only.
	if err := h.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: h.sessionID,
		Update:    acp.StartToolCall(id, call.Name, acp.WithStartKind(kind), acp.WithStartStatus(acp.ToolCallStatusPending), acp.WithStartLocations(locations), acp.WithStartContent(content)),
	}); err != nil {
		return fmt.Errorf("send tool update: %w", err)
	}
	return nil
}

// Decide is the ACP approval policy. A refusal (read_only, reject, failed
// request) returns a deniedError: the model gets a tool error and the turn
// continues. A cancelled request (or cancelled turn) returns the ctx error,
// which aborts the turn.
func (h *permissionHook) Decide(ctx context.Context, call agentcore.ToolCall, tool agentcore.Tool) error {
	if h.conn == nil {
		return &deniedError{cause: ErrPermissionDenied}
	}
	id := acp.ToolCallId(call.CallID)
	h.mu.Lock()
	pending := h.calls[id]
	h.mu.Unlock()
	if pending == nil {
		// Not announced by BeforeTool (should not happen): classify here.
		kind, readTool := toolKind(h.tool(call.Name))
		pending = &pendingCall{kind: kind, readTool: readTool, locations: toolLocations(h.cwd, call.Arguments)}
		pending.content = toolDiff(h.cwd, kind, pending.locations, call.Arguments)
		h.mu.Lock()
		if h.calls == nil {
			h.calls = map[acp.ToolCallId]*pendingCall{}
		}
		h.calls[id] = pending
		h.mu.Unlock()
	}
	kind, readTool := pending.kind, pending.readTool
	if tools.IsMCP(tool) {
		kind, readTool = acp.ToolKindOther, false
	} else if tools.MetaOf(tool).AgentState {
		// Agent-state tools pass even in read_only mode: the tool decides
		// itself whether the write is allowed. Checked before the read-only
		// refusal below; MCP tools never take this path.
		readTool = true
	}
	if h.readOnly && !readTool {
		return h.deny(ctx, id, pending, ErrReadOnly)
	}
	if !readTool {
		decision, err := h.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
			SessionId: h.sessionID,
			ToolCall: acp.ToolCallUpdate{
				ToolCallId: id,
				Title:      acp.Ptr(call.Name),
				Kind:       acp.Ptr(kind),
				Locations:  pending.locations,
				Content:    pending.content,
				Status:     acp.Ptr(acp.ToolCallStatusPending),
			},
			Options: []acp.PermissionOption{
				{Kind: acp.PermissionOptionKindAllowOnce, Name: "Allow once", OptionId: "allow"},
				{Kind: acp.PermissionOptionKindRejectOnce, Name: "Reject", OptionId: "reject"},
			},
		})
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr // the turn was cancelled while asking
			}
			return h.deny(ctx, id, pending, fmt.Errorf("%w: %v", ErrPermissionDenied, err))
		}
		if decision.Outcome.Cancelled != nil {
			return context.Canceled
		}
		if decision.Outcome.Selected == nil || decision.Outcome.Selected.OptionId != "allow" {
			return h.deny(ctx, id, pending, ErrPermissionDenied)
		}
	}
	h.mu.Lock()
	pending.state = callInProgress
	h.mu.Unlock()
	return h.toolStatus(ctx, id, acp.ToolCallStatusInProgress)
}

// deny marks the call refused, reports status failed and returns the refusal.
func (h *permissionHook) deny(ctx context.Context, id acp.ToolCallId, pending *pendingCall, cause error) error {
	h.mu.Lock()
	pending.state = callDenied
	h.mu.Unlock()
	_ = h.toolStatus(ctx, id, acp.ToolCallStatusFailed)
	return &deniedError{cause: cause}
}

// AfterTool closes the call: completed when it ran, nothing more when it was
// refused (failed was already sent), failed when it never reached approval
// (unknown tool, or an unloaded deferred tool answered by visibility).
func (h *permissionHook) AfterTool(ctx context.Context, event *agentcore.ToolEvent, _ *agentcore.Control) error {
	if event == nil || event.Call == nil || h.conn == nil {
		return nil
	}
	id := acp.ToolCallId(event.Call.CallID)
	h.mu.Lock()
	pending := h.calls[id]
	delete(h.calls, id)
	h.mu.Unlock()
	switch {
	case pending == nil || pending.state == callInProgress:
		return h.toolStatus(ctx, id, acp.ToolCallStatusCompleted)
	case pending.state == callDenied:
		return nil
	default:
		return h.toolStatus(ctx, id, acp.ToolCallStatusFailed)
	}
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
		// file_read with paths: point at every file inside the workspace.
		list, err := native.ReadPathList(map[string]any{"paths": args["paths"]})
		if err != nil {
			return nil
		}
		var locations []acp.ToolCallLocation
		for _, p := range list {
			if resolved, err := native.ResolveSandboxedPath(cwd, p); err == nil {
				locations = append(locations, acp.ToolCallLocation{Path: resolved})
			}
		}
		return locations
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

// toolDiff records the proposed replacement for file_write (content) or
// file_edit (old file with the replacement applied) without reading outside
// the workspace.
// It never blocks: only regular files within maxDiffBytes are read, and binary
// or oversized content yields no diff (locations are still reported).
func toolDiff(cwd string, kind acp.ToolKind, locations []acp.ToolCallLocation, args map[string]any) []acp.ToolCallContent {
	if kind != acp.ToolKindEdit || len(locations) == 0 || args == nil {
		return nil
	}
	// file_edit carries old_string/new_string or edits: the new text is the
	// current file with the replacements applied, computed once it is read.
	_, isEdit := args["old_string"]
	if _, multi := args["edits"]; multi {
		isEdit = true
	}
	newText, ok := args["content"].(string)
	if !isEdit && (!ok || len(newText) > maxDiffBytes || !textual(newText)) {
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
	// Open exactly once through os.Root (which follows only in-root symlinks and
	// rejects escapes), then judge the handle itself: no separate Lstat/Stat
	// that a swap could race with.
	f, err := openPreview(root, rel)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// Absent target (including a missing parent directory, which file_write
		// creates) is a new file. A dangling symlink also reports not-exist, so
		// fail closed on anything that is still present as a link.
		if _, lerr := root.Lstat(rel); lerr == nil {
			return nil
		}
	case err != nil:
		// os.Root rejects ".." and symlink escapes with a non-NotExist error.
		return nil
	default:
		defer f.Close()
		before, ok := readBounded(f)
		if !ok {
			return nil
		}
		oldText = acp.Ptr(before)
	}
	if isEdit {
		if oldText == nil {
			return nil // file_edit needs an existing file
		}
		edited, _, err := native.ApplyEdit(*oldText, args)
		if err != nil || len(edited) > maxDiffBytes {
			return nil
		}
		newText = edited
	}
	return []acp.ToolCallContent{{Diff: &acp.ToolCallContentDiff{Type: "diff", Path: locations[0].Path, OldText: oldText, NewText: newText}}}
}

// readBounded reads an opened regular file, giving up on non-regular files
// (FIFO/device/directory), files past maxDiffBytes, or binary content.
func readBounded(f *os.File) (string, bool) {
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxDiffBytes {
		return "", false
	}
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

// tool resolves name in this runtime; nil when unknown or without lookup.
func (h *permissionHook) tool(name string) agentcore.Tool {
	if h.lookup == nil {
		return nil
	}
	return h.lookup(name)
}

// acpKinds maps the neutral tool categories onto ACP tool kinds.
var acpKinds = map[tools.Category]acp.ToolKind{
	tools.CategoryRead:    acp.ToolKindRead,
	tools.CategoryEdit:    acp.ToolKindEdit,
	tools.CategoryDelete:  acp.ToolKindDelete,
	tools.CategoryExecute: acp.ToolKindExecute,
	tools.CategorySearch:  acp.ToolKindSearch,
	tools.CategoryFetch:   acp.ToolKindFetch,
	tools.CategoryThink:   acp.ToolKindThink,
}

// toolKind returns the ACP kind of tool and whether it is read-only (runs
// without a permission request, also in read_only mode). Unknown tools, MCP
// tools and tools that declare nothing are Other and require permission.
func toolKind(tool agentcore.Tool) (acp.ToolKind, bool) {
	meta := tools.MetaOf(tool)
	kind, ok := acpKinds[meta.Category]
	if !ok {
		kind = acp.ToolKindOther
	}
	return kind, meta.ReadOnly
}

func mcpSettings(servers []acp.McpServer) ([]mcp.ServerSettings, error) {
	settings := make([]mcp.ServerSettings, 0, len(servers))
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
		settings = append(settings, mcp.ServerSettings{ID: name, Command: command, Args: append([]string(nil), stdio.Args...), Env: env})
	}
	return settings, nil
}
