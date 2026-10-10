package native

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/pkg/agentcore/api"
)

func TestWebSearch(t *testing.T) {
	var gotNum string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotNum = r.URL.Query().Get("num")
		if r.URL.Query().Get("q") == "fail" {
			_, _ = w.Write([]byte(`{"error":"bad key"}`))
			return
		}
		_, _ = w.Write([]byte(`{"organic_results":[{"title":"Go","link":"https://go.dev","snippet":"` + strings.Repeat("x", 300) + `"}]}`))
	}))
	t.Cleanup(srv.Close)
	tool := NewWebSearchTool("k")
	tool.endpoint = srv.URL
	search := func(args map[string]any) (*api.ToolResult, error) {
		return tool.Execute(context.Background(), api.ToolCall{CallID: "c", Name: "web_search", Arguments: args})
	}

	t.Run("integer num", func(t *testing.T) {
		res, err := search(map[string]any{"query": "go", "num": float64(3)})
		assert.NoErr(t, err)
		assert.Eq(t, "3", gotNum)
		assert.StrContains(t, res.Content, "1. Go\n   https://go.dev")
	})
	t.Run("string num still works", func(t *testing.T) {
		_, err := search(map[string]any{"query": "go", "num": "7"})
		assert.NoErr(t, err)
		assert.Eq(t, "7", gotNum)
	})
	t.Run("default and clamp", func(t *testing.T) {
		_, _ = search(map[string]any{"query": "go"})
		assert.Eq(t, "5", gotNum)
		_, _ = search(map[string]any{"query": "go", "num": 500})
		assert.Eq(t, "20", gotNum)
	})
	t.Run("schema says integer", func(t *testing.T) {
		props := tool.Schema().InputSchema["properties"].(map[string]any)
		assert.Eq(t, "integer", props["num"].(map[string]any)["type"])
	})
	t.Run("api error", func(t *testing.T) {
		_, err := search(map[string]any{"query": "fail"})
		assert.ErrMsgContains(t, err, "bad key")
	})
	t.Run("output is capped", func(t *testing.T) {
		tool.maxOut = 100
		t.Cleanup(func() { tool.maxOut = 0 })
		res, err := search(map[string]any{"query": "go"})
		assert.NoErr(t, err)
		assert.StrContains(t, res.Content, "省略")
	})
}
