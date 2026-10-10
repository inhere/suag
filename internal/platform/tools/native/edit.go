package native

import (
	"context"
	"fmt"
	"strings"

	"github.com/inhere/suag/pkg/agentcore/api"
)

// FileEditTool replaces an exact string in a workspace file.
type FileEditTool struct{ workspace string }

func (t *FileEditTool) Name() string { return "file_edit" }
func (t *FileEditTool) Description() string {
	return "Edit a workspace file by replacing an exact string. Read the file first: old_string must match the " +
		"file text exactly (without file_read's line-number prefix) and be unique unless replace_all is set; " +
		"include surrounding lines to make it unique. The file's line endings (LF/CRLF) are kept. " +
		"Prefer this over rewriting the whole file with file_write."
}
func (t *FileEditTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":        map[string]any{"type": "string", "description": "Path within workspace (relative, or absolute inside it)"},
				"old_string":  map[string]any{"type": "string", "description": "Exact text to replace"},
				"new_string":  map[string]any{"type": "string", "description": "Replacement text"},
				"replace_all": map[string]any{"type": "boolean", "description": "Replace every occurrence (default false)"},
			},
			"required": []any{"path", "old_string", "new_string"},
		},
	}
}

func (t *FileEditTool) Execute(_ context.Context, call api.ToolCall) (*api.ToolResult, error) {
	path, err := sandboxedPath(t.workspace, call.Arguments, "path")
	if err != nil {
		return nil, err
	}
	data, err := readFileInRoot(t.workspace, path)
	if err != nil {
		return nil, fmt.Errorf("file_edit: %w", err)
	}
	updated, n, err := ApplyEdit(string(data), call.Arguments)
	if err != nil {
		return nil, fmt.Errorf("file_edit %s: %w", path, err)
	}
	if err := writeFileInRoot(t.workspace, path, []byte(updated)); err != nil {
		return nil, fmt.Errorf("file_edit: %w", err)
	}
	return &api.ToolResult{
		CallID:  call.CallID,
		Name:    t.Name(),
		Content: fmt.Sprintf("edited %s: %d replacement(s)", path, n),
	}, nil
}

// ApplyEdit applies file_edit's arguments (old_string, new_string,
// replace_all) to content and returns the new content and the number of
// replacements. When the file uses CRLF and the strings use bare LF, the
// strings are converted so the file keeps CRLF. Exported so ACP can preview
// the exact result as a diff.
func ApplyEdit(content string, args map[string]any) (string, int, error) {
	oldStr, _ := args["old_string"].(string)
	newStr, _ := args["new_string"].(string)
	replaceAll, _ := args["replace_all"].(bool)
	if oldStr == "" {
		return "", 0, fmt.Errorf("old_string is required (use file_write to create a file)")
	}
	if oldStr == newStr {
		return "", 0, fmt.Errorf("old_string and new_string are identical")
	}
	if strings.Contains(content, "\r\n") && !strings.Contains(oldStr, "\r\n") {
		oldStr = strings.ReplaceAll(oldStr, "\n", "\r\n")
		newStr = strings.ReplaceAll(strings.ReplaceAll(newStr, "\r\n", "\n"), "\n", "\r\n")
	}
	n := strings.Count(content, oldStr)
	switch {
	case n == 0:
		return "", 0, fmt.Errorf("old_string not found; re-read the file and copy the text exactly (whitespace and indentation included)")
	case n > 1 && !replaceAll:
		return "", 0, fmt.Errorf("old_string matches %d places; add surrounding lines to make it unique or set replace_all", n)
	}
	if replaceAll {
		return strings.ReplaceAll(content, oldStr, newStr), n, nil
	}
	return strings.Replace(content, oldStr, newStr, 1), 1, nil
}
