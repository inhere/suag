// Package native provides built-in tool implementations.
package native

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/inhere/suga/pkg/agentcore/api"
	"github.com/inhere/suga/pkg/agentcore/tools"
)

// FileWriteTool writes/creates a file within the workspace, or under a
// directory the session added as read-write (--add-dir ...:rw).
type FileWriteTool struct {
	workspace string
	addDirs   *AddDirs
}

// NewFileTools returns the three file tools scoped to workspace.
func NewFileTools(workspace string) []api.Tool {
	return []api.Tool{
		&FileReadTool{workspace: workspace},
		&FileWriteTool{workspace: workspace},
		&FileListTool{workspace: workspace},
	}
}

// ─── FileWriteTool ────────────────────────────────────────────────────────────

func (t *FileWriteTool) Name() string { return "file_write" }
func (t *FileWriteTool) Description() string {
	// The wording is exact: writeFileInRoot writes the decoded content's bytes
	// through os.Root.OpenFile with O_TRUNC (binary, no BOM, no newline
	// translation), so a caller can rely on byte-identical content.
	return "Write content to a file in the workspace. Creates parent directories as needed. " +
		"The file is written as UTF-8 without a BOM, with the line endings and bytes of content unchanged."
}

// ToolMeta marks the tool as an edit: it changes workspace files.
func (*FileWriteTool) ToolMeta() tools.Meta {
	return tools.Meta{Category: tools.CategoryEdit, ReadOnly: false}
}
func (t *FileWriteTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path within the workspace (relative, or absolute inside it), or an absolute path under a directory added as read-write",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "File content to write",
				},
			},
			"required": []any{"path", "content"},
		},
	}
}

func (t *FileWriteTool) Execute(_ context.Context, call api.ToolCall) (*api.ToolResult, error) {
	target, err := writeTargetPath(t.workspace, t.addDirs, call.Arguments)
	if err != nil {
		return nil, err
	}
	content, _ := call.Arguments["content"].(string)

	if err := writeFileInRoot(target.base, target.abs, []byte(content)); err != nil {
		return nil, fmt.Errorf("file_write: %w", err)
	}
	return &api.ToolResult{
		CallID:  call.CallID,
		Name:    t.Name(),
		Content: fmt.Sprintf("wrote %d bytes to %s", len(content), target.abs),
	}, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// ResolveSandboxedPath is the path rule shared by the file tools. A relative
// rel is joined under workspace; an absolute rel is accepted only when it lies
// inside workspace (Windows compares volume and names case-insensitively) and
// is otherwise rejected. Either way the result must not escape workspace.
// Exported so ACP reports exactly the path the tool writes.
func ResolveSandboxedPath(workspace, rel string) (string, error) {
	workspace = filepath.Clean(workspace)
	var abs string
	if filepath.IsAbs(rel) {
		mapped, ok := absUnderWorkspace(workspace, rel, foldPathCase)
		if !ok {
			return "", fmt.Errorf("path escape: %q is outside workspace", rel)
		}
		abs = mapped
	} else if filepath.VolumeName(rel) != "" {
		// "C:foo" is drive-relative: it names no place inside the workspace.
		return "", fmt.Errorf("path escape: %q is outside workspace", rel)
	} else {
		abs = filepath.Join(workspace, filepath.Clean(rel))
	}
	// Security: reject any path that escapes the workspace root. Compare via
	// filepath.Rel so a sibling like /a/ws2 is not mistaken for inside /a/ws
	// (a bare string-prefix check would accept it).
	if _, err := relInWorkspace(workspace, abs); err != nil {
		return "", fmt.Errorf("path escape: %q is outside workspace", rel)
	}
	return abs, nil
}

// relInWorkspace returns abs relative to workspace, or an error when abs is
// not workspace itself or a descendant of it.
func relInWorkspace(workspace, abs string) (string, error) {
	rel, err := filepath.Rel(workspace, abs)
	if err != nil || filepath.IsAbs(rel) || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escape: %q is outside workspace", abs)
	}
	return rel, nil
}

// openWorkspaceRoot opens workspace as an os.Root. All real file I/O goes
// through it so symlinks that point outside the workspace are rejected by the
// OS-level traversal check, while symlinks that stay inside keep working.
func openWorkspaceRoot(workspace string) (*os.Root, error) {
	return os.OpenRoot(filepath.Clean(workspace))
}

// wrapRootErr rewords err into the "path escape" wording used by the lexical
// check. Call it only after realRelInRoot confirmed the escape; it does not
// inspect err itself.
func wrapRootErr(err error, path string) error {
	return fmt.Errorf("path escape: %q is outside workspace (via symlink): %w", path, err)
}

func readFileInRoot(workspace, abs string) ([]byte, error) {
	return withRoot(workspace, abs, func(root *os.Root, rel string) ([]byte, error) {
		f, err := root.Open(rel)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return io.ReadAll(f)
	})
}

func writeFileInRoot(workspace, abs string, data []byte) error {
	_, err := withRoot(workspace, abs, func(root *os.Root, rel string) (struct{}, error) {
		if dir := filepath.Dir(rel); dir != "." {
			if err := root.MkdirAll(dir, 0o755); err != nil {
				return struct{}{}, fmt.Errorf("mkdir: %w", err)
			}
		}
		f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return struct{}{}, err
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			return struct{}{}, err
		}
		return struct{}{}, f.Close()
	})
	return err
}

// writeTargetPath resolves the "path" argument of a write tool against the
// workspace and the session's read-write directories (--add-dir ...:rw), and
// nothing else: a path outside both is refused.
func writeTargetPath(workspace string, dirs *AddDirs, args map[string]any) (readTarget, error) {
	rel, _ := args["path"].(string)
	if rel == "" {
		return readTarget{}, fmt.Errorf("%q arg is required", "path")
	}
	return resolveWrite(workspace, dirs.WritePaths(), rel)
}
