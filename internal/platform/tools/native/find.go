package native

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/pkg/agentcore/api"
)

const defaultFindResults = 200

// FileFindTool finds files by glob pattern in the workspace.
type FileFindTool struct {
	workspace string
	maxOut    int // result cap in bytes; 0 = DefaultMaxOutputBytes
}

func (t *FileFindTool) Name() string     { return "file_find" }
func (t *FileFindTool) Kind() tools.Kind { return tools.KindNative }
func (t *FileFindTool) Description() string {
	return "Find files in the workspace by glob pattern (** matches any directories, e.g. **/*_test.go; " +
		"a pattern without / matches file names at any depth). Newest first; paths relative to the workspace. " +
		"Skips .git, node_modules, vendor, dist and .gitignore matches."
}
func (t *FileFindTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":     map[string]any{"type": "string", "description": "Glob, e.g. *.md or cmd/**/main.go"},
				"path":        map[string]any{"type": "string", "description": "Directory to search (default: workspace root)"},
				"max_results": map[string]any{"type": "integer", "description": fmt.Sprintf("Maximum paths (default %d)", defaultFindResults)},
			},
			"required": []any{"pattern"},
		},
	}
}

func (t *FileFindTool) Execute(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	pattern, _ := call.Arguments["pattern"].(string)
	if pattern == "" {
		return nil, fmt.Errorf("file_find: pattern is required")
	}
	start, err := searchStart(t.workspace, call.Arguments)
	if err != nil {
		return nil, err
	}
	limit := defaultFindResults
	if n, ok := numberArg(call.Arguments["max_results"]); ok && n >= 1 {
		limit = int(n)
	}

	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	type hit struct {
		rel string
		mod time.Time
	}
	var hits []hit
	timedOut, err := walkWorkspace(ctx, t.workspace, start, func(_ *os.Root, rel string, d fs.DirEntry) error {
		if !globMatch(pattern, rel) {
			return nil
		}
		var mod time.Time
		if info, err := d.Info(); err == nil {
			mod = info.ModTime()
		}
		hits = append(hits, hit{rel: rel, mod: mod})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("file_find: %w", err)
	}
	// Newest first; ties by path so the order is stable.
	sort.Slice(hits, func(i, j int) bool {
		if !hits[i].mod.Equal(hits[j].mod) {
			return hits[i].mod.After(hits[j].mod)
		}
		return hits[i].rel < hits[j].rel
	})
	total := len(hits)
	if total > limit {
		hits = hits[:limit]
	}
	lines := make([]string, len(hits))
	for i, h := range hits {
		lines[i] = h.rel
	}
	content := strings.Join(lines, "\n")
	if total == 0 {
		content = "no files found"
	}
	if total > limit {
		content += fmt.Sprintf("\n[showing %d of %d; narrow the pattern or path]", limit, total)
	}
	if timedOut {
		content += fmt.Sprintf("\n[已超时（%s），结果不完整]", searchTimeout)
	}
	return &api.ToolResult{CallID: call.CallID, Name: t.Name(), Content: TruncateHeadTail(content, t.maxOut)}, nil
}
