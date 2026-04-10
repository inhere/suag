// Package native provides built-in tool implementations.
package native

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/internal/platform/tools"
)

// FileReadTool reads a file within the workspace.
type FileReadTool struct{ workspace string }

// FileWriteTool writes/creates a file within the workspace.
type FileWriteTool struct{ workspace string }

// FileListTool lists files in a workspace directory.
type FileListTool struct{ workspace string }

// NewFileTools returns the three file tools scoped to workspace.
func NewFileTools(workspace string) []tools.Tool {
	return []tools.Tool{
		&FileReadTool{workspace: workspace},
		&FileWriteTool{workspace: workspace},
		&FileListTool{workspace: workspace},
	}
}

// ─── FileReadTool ─────────────────────────────────────────────────────────────

func (t *FileReadTool) Name() string        { return "file_read" }
func (t *FileReadTool) Kind() tools.Kind    { return tools.KindNative }
func (t *FileReadTool) Description() string { return "Read the contents of a file in the workspace." }
func (t *FileReadTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: llm.SchemaProperty{
			Type: "object",
			Properties: map[string]*llm.SchemaProperty{
				"path": {Type: "string", Description: "Relative path within workspace"},
			},
			Required: []string{"path"},
		},
	}
}

func (t *FileReadTool) Execute(_ context.Context, args map[string]any) (string, error) {
	path, err := sandboxedPath(t.workspace, args, "path")
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("file_read: %w", err)
	}
	return string(data), nil
}

// ─── FileWriteTool ────────────────────────────────────────────────────────────

func (t *FileWriteTool) Name() string     { return "file_write" }
func (t *FileWriteTool) Kind() tools.Kind { return tools.KindNative }
func (t *FileWriteTool) Description() string {
	return "Write content to a file in the workspace. Creates parent directories as needed."
}
func (t *FileWriteTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: llm.SchemaProperty{
			Type: "object",
			Properties: map[string]*llm.SchemaProperty{
				"path":    {Type: "string", Description: "Relative path within workspace"},
				"content": {Type: "string", Description: "File content to write"},
			},
			Required: []string{"path", "content"},
		},
	}
}

func (t *FileWriteTool) Execute(_ context.Context, args map[string]any) (string, error) {
	path, err := sandboxedPath(t.workspace, args, "path")
	if err != nil {
		return "", err
	}
	content, _ := args["content"].(string)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("file_write mkdir: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("file_write: %w", err)
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil
}

// ─── FileListTool ─────────────────────────────────────────────────────────────

func (t *FileListTool) Name() string        { return "file_list" }
func (t *FileListTool) Kind() tools.Kind    { return tools.KindNative }
func (t *FileListTool) Description() string { return "List files in a workspace directory." }
func (t *FileListTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: llm.SchemaProperty{
			Type: "object",
			Properties: map[string]*llm.SchemaProperty{
				"dir": {Type: "string", Description: "Relative directory path (empty = workspace root)"},
			},
		},
	}
}

func (t *FileListTool) Execute(_ context.Context, args map[string]any) (string, error) {
	dirArg, _ := args["dir"].(string)
	if dirArg == "" {
		dirArg = "."
	}
	base := filepath.Join(t.workspace, filepath.Clean(dirArg))
	if !strings.HasPrefix(base, t.workspace) {
		return "", fmt.Errorf("path escape: %q is outside workspace", dirArg)
	}

	entries, err := os.ReadDir(base)
	if err != nil {
		return "", fmt.Errorf("file_list: %w", err)
	}
	var sb strings.Builder
	for _, e := range entries {
		if e.IsDir() {
			sb.WriteString(e.Name() + "/\n")
		} else {
			sb.WriteString(e.Name() + "\n")
		}
	}
	return sb.String(), nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// sandboxedPath resolves the named arg as a path confined to workspace.
func sandboxedPath(workspace string, args map[string]any, key string) (string, error) {
	rel, _ := args[key].(string)
	if rel == "" {
		return "", fmt.Errorf("%q arg is required", key)
	}
	abs := filepath.Join(workspace, filepath.Clean(rel))
	// Security: reject any path that escapes the workspace root.
	if !strings.HasPrefix(abs, workspace) {
		return "", fmt.Errorf("path escape: %q is outside workspace", rel)
	}
	return abs, nil
}
