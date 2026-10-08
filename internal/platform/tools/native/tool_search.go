package native

import (
	"context"
	"fmt"
	"strings"

	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/pkg/agentcore/api"
)

// ToolSearcher finds deferred tools and loads the matches (tools.Visibility).
type ToolSearcher interface {
	Search(ctx context.Context, query string, limit int) (tools.SearchResult, error)
}

// ToolSearchTool lets the model load deferred MCP tools. It is registered only
// when some tools are deferred; with nothing deferred it would be a useless
// entry in every request's tool prefix.
type ToolSearchTool struct {
	searcher ToolSearcher
}

// NewToolSearchTool creates tool_search backed by searcher.
func NewToolSearchTool(searcher ToolSearcher) *ToolSearchTool {
	return &ToolSearchTool{searcher: searcher}
}

func (t *ToolSearchTool) Name() string     { return "tool_search" }
func (t *ToolSearchTool) Kind() tools.Kind { return tools.KindNative }
func (t *ToolSearchTool) Description() string {
	return "Search the deferred MCP tools listed in the system prompt and load the matches so they can be " +
		"called from the next step. query is keywords, or \"select:name1,name2\" to load exact tools."
}

func (t *ToolSearchTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Keywords, or select:name1,name2 for exact tools"},
				"limit": map[string]any{"type": "integer", "description": "Max results (default 5, at most 10)"},
			},
			"required": []any{"query"},
		},
	}
}

func (t *ToolSearchTool) Execute(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	query, _ := call.Arguments["query"].(string)
	limit := tools.DefaultSearchLimit
	switch n := call.Arguments["limit"].(type) {
	case float64:
		limit = int(n)
	case int:
		limit = n
	case int64:
		limit = int(n)
	}
	res, err := t.searcher.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	if len(res.Hits) == 0 {
		b.WriteString("No deferred tool matched. Try other keywords or select:<exact name>.")
	} else {
		fmt.Fprintf(&b, "Found %d tool(s); they can be called from the next step:\n", len(res.Hits))
		for _, h := range res.Hits {
			state := "newly loaded"
			if h.AlreadyLoaded {
				state = "already loaded"
			}
			fmt.Fprintf(&b, "- %s [%s] — %s\n", h.Name, state, strings.Join(strings.Fields(h.Description), " "))
		}
	}
	if len(res.Missing) > 0 {
		fmt.Fprintf(&b, "\nNot deferred tools: %s", strings.Join(res.Missing, ", "))
	}
	return &api.ToolResult{CallID: call.CallID, Name: t.Name(), Content: strings.TrimRight(b.String(), "\n")}, nil
}
