package native

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore/api"
	"github.com/inhere/suga/pkg/agentcore/tools"
)

type fakeSearcher struct {
	query string
	limit int
	res   tools.SearchResult
}

func (f *fakeSearcher) Search(_ context.Context, q string, limit int) (tools.SearchResult, error) {
	f.query, f.limit = q, limit
	return f.res, nil
}

func TestToolSearchTool(t *testing.T) {
	ctx := context.Background()
	t.Run("formats hits and passes limit", func(t *testing.T) {
		s := &fakeSearcher{res: tools.SearchResult{
			Hits:    []tools.SearchHit{{Name: "a_tool", Description: "does  a\nthing"}, {Name: "b_tool", AlreadyLoaded: true}},
			Missing: []string{"zzz"},
		}}
		res, err := NewToolSearchTool(s).Execute(ctx, api.ToolCall{CallID: "c1", Arguments: map[string]any{"query": "select:a_tool", "limit": float64(7)}})
		assert.NoErr(t, err)
		assert.Eq(t, "select:a_tool", s.query)
		assert.Eq(t, 7, s.limit)
		assert.StrContains(t, res.Content, "- a_tool [newly loaded] — does a thing")
		assert.StrContains(t, res.Content, "- b_tool [already loaded]")
		assert.StrContains(t, res.Content, "zzz")
	})
	t.Run("default limit and no match", func(t *testing.T) {
		s := &fakeSearcher{}
		res, err := NewToolSearchTool(s).Execute(ctx, api.ToolCall{Arguments: map[string]any{"query": "x"}})
		assert.NoErr(t, err)
		assert.Eq(t, tools.DefaultSearchLimit, s.limit)
		assert.StrContains(t, res.Content, "No deferred tool matched")
	})
	t.Run("schema", func(t *testing.T) {
		tool := NewToolSearchTool(nil)
		assert.Eq(t, "tool_search", tool.Schema().Name)
		assert.Eq(t, tools.KindNative, tools.KindOf(tool))
	})
}
