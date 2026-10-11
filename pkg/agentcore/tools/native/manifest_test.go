package native

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/pkg/agentcore/api"
	"github.com/inhere/suag/pkg/agentcore/tools"
)

func TestManifestTool(t *testing.T) {
	call := api.ToolCall{CallID: "c1"}

	t.Run("meta is read-only and not external", func(t *testing.T) {
		m := tools.MetaOf(NewManifestTool(nil))
		assert.True(t, m.ReadOnly)
		assert.False(t, m.External)
		assert.Eq(t, tools.CategoryRead, m.Category)
		assert.Eq(t, "suag_manifest", NewManifestTool(nil).Schema().Name)
	})
	t.Run("struct value is encoded as JSON", func(t *testing.T) {
		tool := NewManifestTool(func(context.Context) (any, error) {
			return map[string]any{"version": "1.2.3", "tools": []string{"a"}}, nil
		})
		res, err := tool.Execute(context.Background(), call)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "suag_manifest", res.Name)
		assert.Eq(t, "c1", res.CallID)
		var got map[string]any
		assert.NoErr(t, json.Unmarshal([]byte(res.Content), &got))
		assert.Eq(t, "1.2.3", got["version"])
	})
	t.Run("section returns one top-level key", func(t *testing.T) {
		tool := NewManifestTool(func(context.Context) (any, error) {
			return map[string]any{"version": "1.2.3", "tools": []string{"a"}}, nil
		})
		res, err := tool.Execute(context.Background(), api.ToolCall{CallID: "c2", Arguments: map[string]any{"section": "tools"}})
		assert.Require(t, assert.NoErr(t, err))
		var got []string
		assert.NoErr(t, json.Unmarshal([]byte(res.Content), &got))
		assert.Eq(t, []string{"a"}, got)

		_, err = tool.Execute(context.Background(), api.ToolCall{CallID: "c3", Arguments: map[string]any{"section": "nope"}})
		assert.Err(t, err)
		assert.StrContains(t, err.Error(), "tools, version")
	})
	t.Run("string value is passed through", func(t *testing.T) {
		res, err := NewManifestTool(func(context.Context) (any, error) { return `{"a":1}`, nil }).Execute(context.Background(), call)
		assert.NoErr(t, err)
		assert.Eq(t, `{"a":1}`, res.Content)
	})
	t.Run("errors", func(t *testing.T) {
		_, err := NewManifestTool(nil).Execute(context.Background(), call)
		assert.Err(t, err)
		_, err = NewManifestTool(func(context.Context) (any, error) { return nil, errors.New("boom") }).Execute(context.Background(), call)
		assert.Err(t, err)
		assert.StrContains(t, err.Error(), "boom")
	})
}
