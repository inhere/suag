package native

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/inhere/suag/pkg/agentcore/api"
	"github.com/inhere/suag/pkg/agentcore/tools"
)

// FileEditTool replaces exact strings in a workspace file: one replacement
// (old_string/new_string) or several in one call (edits).
type FileEditTool struct{ workspace string }

func (t *FileEditTool) Name() string { return "file_edit" }
func (t *FileEditTool) Description() string {
	return "Edit a workspace file by replacing an exact string. Read the file first: old_string must match the " +
		"file text exactly (without file_read's line-number prefix) and be unique unless replace_all is set; " +
		"include surrounding lines to make it unique. For several changes to the same file pass them all in " +
		"edits (one call, applied in order, all or nothing) instead of old_string/new_string. " +
		"The file's line endings (LF/CRLF) are kept. Prefer this over rewriting the whole file with file_write."
}

// ToolMeta marks the tool as an edit: it changes workspace files.
func (*FileEditTool) ToolMeta() tools.Meta {
	return tools.Meta{Category: tools.CategoryEdit, ReadOnly: false}
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
				"edits": map[string]any{
					"type":        "array",
					"description": "Several replacements, each applied to the result of the previous one; if any fails the file is left unchanged. Use instead of old_string/new_string",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"old_string":  map[string]any{"type": "string"},
							"new_string":  map[string]any{"type": "string"},
							"replace_all": map[string]any{"type": "boolean"},
						},
						"required": []any{"old_string", "new_string"},
					},
				},
			},
			"required": []any{"path"},
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
// replace_all, or the edits list) to content and returns the new content and
// the number of replacements. Edits apply in order to the previous result and
// any failure aborts the whole call. When the file uses CRLF and the strings
// use bare LF, the strings are converted so the file keeps CRLF. Exported so
// ACP can preview the exact result as a diff.
func ApplyEdit(content string, args map[string]any) (string, int, error) {
	edits, err := EditList(args)
	if err != nil {
		return "", 0, err
	}
	total := 0
	for i, edit := range edits {
		var n int
		content, n, err = applyOne(content, edit)
		if err != nil {
			if len(edits) > 1 {
				err = fmt.Errorf("edits[%d]: %w (no edit was applied)", i, err)
			}
			return "", 0, err
		}
		total += n
	}
	return content, total, nil
}

// EditList returns file_edit's replacements as a list: the edits argument,
// or the single old_string/new_string pair. edits may arrive as a JSON string
// (some models encode nested arrays that way); mixing both forms is an error.
func EditList(args map[string]any) ([]map[string]any, error) {
	raw, ok := args["edits"]
	if !ok || raw == nil {
		return []map[string]any{args}, nil
	}
	if _, single := args["old_string"]; single {
		return nil, fmt.Errorf("pass either edits or old_string/new_string, not both")
	}
	if text, isText := raw.(string); isText {
		if err := json.Unmarshal([]byte(text), &raw); err != nil {
			return nil, fmt.Errorf("edits must be an array of {old_string, new_string, replace_all}: %w", err)
		}
	}
	items, _ := raw.([]any)
	if len(items) == 0 {
		return nil, fmt.Errorf("edits must be a non-empty array of {old_string, new_string, replace_all}")
	}
	edits := make([]map[string]any, len(items))
	for i, item := range items {
		edit, isObj := item.(map[string]any)
		if !isObj {
			return nil, fmt.Errorf("edits[%d] must be an object with old_string and new_string", i)
		}
		edits[i] = edit
	}
	return edits, nil
}

// applyOne applies a single replacement (old_string, new_string, replace_all).
func applyOne(content string, args map[string]any) (string, int, error) {
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
