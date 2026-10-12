package native

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/inhere/suag/pkg/agentcore/api"
	"github.com/inhere/suag/pkg/agentcore/tools"
)

const (
	defaultListEntries = 500
	maxListDepth       = 5
)

// ignoredDirs are listed but never descended into: they are large and rarely
// what the model is looking for.
var ignoredDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true}

// FileListTool lists files in a workspace directory, optionally recursively.
type FileListTool struct {
	workspace string
	readRoots []string // read-only directories outside the workspace
	maxOut    int      // raw result cap in bytes; 0 = DefaultRawOutputBytes
}

func (t *FileListTool) Name() string { return "file_list" }
func (t *FileListTool) Description() string {
	return "List a workspace directory. Directories end with /, symlinks with @ (not followed). " +
		"depth>1 lists recursively as relative paths; .git, node_modules, vendor and dist are shown but not entered."
}

// ToolMeta marks the tool as a read-only read.
func (*FileListTool) ToolMeta() tools.Meta {
	return tools.Meta{Category: tools.CategoryRead, ReadOnly: true}
}
func (t *FileListTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"dir": map[string]any{
					"type":        "string",
					"description": "Relative directory path (empty = workspace root), or an absolute one under a read-only dependency root",
				},
				"depth": map[string]any{
					"type":        "integer",
					"description": fmt.Sprintf("Levels to list (default 1, max %d)", maxListDepth),
				},
				"max_entries": map[string]any{
					"type":        "integer",
					"description": fmt.Sprintf("Maximum entries to return (default %d)", defaultListEntries),
				},
			},
		},
	}
}

func (t *FileListTool) Execute(_ context.Context, call api.ToolCall) (*api.ToolResult, error) {
	dirArg, _ := call.Arguments["dir"].(string)
	if dirArg == "" {
		dirArg = "."
	}
	target, err := resolveRead(t.workspace, t.readRoots, dirArg)
	if err != nil {
		return nil, err
	}
	depth, maxEntries := 1, defaultListEntries
	if n, ok := numberArg(call.Arguments["depth"]); ok && n >= 1 {
		depth = min(int(n), maxListDepth)
	}
	if n, ok := numberArg(call.Arguments["max_entries"]); ok && n >= 1 {
		maxEntries = int(n)
	}

	list, err := withRoot(target.base, target.abs, func(root *os.Root, rel string) (listing, error) {
		var out listing
		err := listDir(root, rel, "", depth, maxEntries, &out)
		return out, err
	})
	if err != nil {
		return nil, fmt.Errorf("file_list: %w", err)
	}
	content := strings.Join(list.entries, "\n")
	if list.truncated {
		content += fmt.Sprintf("\n[truncated at %d entries; narrow dir or depth]", maxEntries)
	}
	return &api.ToolResult{
		CallID:  call.CallID,
		Name:    t.Name(),
		Content: TruncateHeadTail(content, rawLimit(t.maxOut)),
	}, nil
}

type listing struct {
	entries   []string
	truncated bool
}

// listDir appends the entries of dir (relative to root) to out, prefixed by
// prefix, descending while depth > 1. Symlinks are never followed and
// os.Root keeps every open inside the workspace.
func listDir(root *os.Root, dir, prefix string, depth, maxEntries int, out *listing) error {
	d, err := root.Open(dir)
	if err != nil {
		return err
	}
	entries, err := d.ReadDir(-1)
	d.Close()
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		if len(out.entries) >= maxEntries {
			out.truncated = true
			return nil
		}
		name := path.Join(prefix, e.Name())
		switch {
		case e.Type()&os.ModeSymlink != 0:
			out.entries = append(out.entries, name+"@")
		case e.IsDir():
			out.entries = append(out.entries, name+"/")
			if depth > 1 && !ignoredDirs[e.Name()] {
				if err := listDir(root, filepath.Join(dir, e.Name()), name, depth-1, maxEntries, out); err != nil {
					return err
				}
			}
		default:
			out.entries = append(out.entries, name)
		}
	}
	return nil
}
