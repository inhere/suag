package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/pkg/agentcore/api"
)

// fakeMCP is a KindMCP tool with a configurable server, description and schema size.
type fakeMCP struct {
	name, desc, server string
	pad                int
	calls              *int
}

func (f *fakeMCP) Name() string        { return f.name }
func (f *fakeMCP) Description() string { return f.desc }
func (f *fakeMCP) Kind() Kind          { return KindMCP }
func (f *fakeMCP) MCPServer() string   { return f.server }
func (f *fakeMCP) Schema() api.ToolSchema {
	return api.ToolSchema{Name: f.name, Description: f.desc, InputSchema: map[string]any{
		"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string", "description": strings.Repeat("x", f.pad)}},
	}}
}
func (f *fakeMCP) Execute(_ context.Context, call api.ToolCall) (*api.ToolResult, error) {
	if f.calls != nil {
		*f.calls++
	}
	return &api.ToolResult{CallID: call.CallID, Name: f.name, Content: "ran " + f.name}, nil
}

// thirtyMCP builds a registry of 2 native tools and 30 MCP tools (srv_a/srv_b).
func thirtyMCP(pad int, calls *int) *Registry {
	reg := NewRegistry()
	reg.Register(&mockTool{name: "file_read", result: "r"})
	reg.Register(&mockTool{name: "update_plan", result: "p"})
	for i := 0; i < 30; i++ {
		server := "srv_a"
		if i%2 == 1 {
			server = "srv_b"
		}
		reg.Register(&fakeMCP{name: fmt.Sprintf("tool_%02d", i), server: server, pad: pad, calls: calls,
			desc: fmt.Sprintf("does thing number %d for %s", i, server)})
	}
	reg.Register(&fakeMCP{name: "gofer_issue_list", server: "gofer", desc: "List issues in the tracker"})
	reg.Register(&fakeMCP{name: "gofer_issue_close", server: "gofer", desc: "Close an issue"})
	return reg
}

func names(schemas []api.ToolSchema) []string {
	out := make([]string, 0, len(schemas))
	for _, s := range schemas {
		out = append(out, s.Name)
	}
	return out
}

func TestVisibilityModes(t *testing.T) {
	ctx := context.Background()
	t.Run("never lists everything", func(t *testing.T) {
		v := NewVisibility(thirtyMCP(10, nil), VisibilityOptions{Mode: DeferredNever})
		assert.Eq(t, false, v.Active())
		got, _ := v.ListTools(ctx)
		assert.Eq(t, 34, len(got))
	})
	t.Run("always defers all MCP tools", func(t *testing.T) {
		v := NewVisibility(thirtyMCP(10, nil), VisibilityOptions{Mode: DeferredAlways})
		assert.Eq(t, true, v.Active())
		got, _ := v.ListTools(ctx)
		assert.Eq(t, []string{"file_read", "update_plan"}, names(got))
	})
	t.Run("always without MCP tools is inactive", func(t *testing.T) {
		reg := NewRegistry()
		reg.Register(&mockTool{name: "file_read"})
		assert.Eq(t, false, NewVisibility(reg, VisibilityOptions{Mode: DeferredAlways}).Active())
	})
	t.Run("auto threshold", func(t *testing.T) {
		reg := thirtyMCP(200, nil)
		total := 0
		for _, s := range reg.Schemas() {
			if tool, _ := reg.Get(s.Name); KindOf(tool) == KindMCP {
				total += len(schemaJSON(s))
			}
		}
		// threshold = window*4*10% ; window chosen so threshold sits just either side of total.
		below := total*100/(bytesPerToken*autoDeferPercent) + 5 // threshold > total: not deferred
		above := total*100/(bytesPerToken*autoDeferPercent) - 5 // threshold < total: deferred
		assert.Eq(t, false, NewVisibility(reg, VisibilityOptions{Mode: DeferredAuto, ContextWindow: below}).Active())
		assert.Eq(t, true, NewVisibility(reg, VisibilityOptions{Mode: DeferredAuto, ContextWindow: above}).Active())
		// Default mode string is auto.
		m, err := ParseDeferredMode("")
		assert.NoErr(t, err)
		assert.Eq(t, DeferredAuto, m)
		_, err = ParseDeferredMode("bogus")
		assert.Err(t, err)
	})
}

func TestVisibilitySearchAndListing(t *testing.T) {
	ctx := context.Background()
	v := NewVisibility(thirtyMCP(10, nil), VisibilityOptions{Mode: DeferredAlways})
	var events []SearchEvent
	v.Bind(nil, func(_ context.Context, ev SearchEvent) { events = append(events, ev) })

	t.Run("select exact appends sorted after direct", func(t *testing.T) {
		res, err := v.Search(ctx, "select:tool_07, tool_03,nope", 5)
		assert.NoErr(t, err)
		assert.Eq(t, []string{"tool_07", "tool_03"}, res.NewlyLoaded)
		assert.Eq(t, []string{"nope"}, res.Missing)
		got, _ := v.ListTools(ctx)
		assert.Eq(t, []string{"file_read", "update_plan", "tool_03", "tool_07"}, names(got))
	})
	t.Run("keyword search ranks name over description", func(t *testing.T) {
		res, err := v.Search(ctx, "issue list", 5)
		assert.NoErr(t, err)
		assert.Eq(t, "gofer_issue_list", res.Hits[0].Name)
		assert.Eq(t, "gofer_issue_close", res.Hits[1].Name)
		got, _ := v.ListTools(ctx)
		assert.Eq(t, []string{"file_read", "update_plan", "gofer_issue_close", "gofer_issue_list", "tool_03", "tool_07"}, names(got))
	})
	t.Run("limit caps results and already loaded is flagged", func(t *testing.T) {
		res, err := v.Search(ctx, "thing", 3)
		assert.NoErr(t, err)
		assert.Eq(t, 3, len(res.Hits))
		res, err = v.Search(ctx, "thing", 100)
		assert.NoErr(t, err)
		assert.Eq(t, MaxSearchLimit, len(res.Hits))
		res, _ = v.Search(ctx, "select:tool_03", 1)
		assert.Eq(t, true, res.Hits[0].AlreadyLoaded)
		assert.Eq(t, 0, len(res.NewlyLoaded))
	})
	t.Run("events carry invalidation only on change", func(t *testing.T) {
		first := events[0]
		assert.Eq(t, SearchEventSearch, first.Kind)
		assert.Eq(t, true, first.CacheInvalidated)
		last := events[len(events)-1]
		assert.Eq(t, false, last.CacheInvalidated)
		assert.Eq(t, "select:tool_03", last.Query)
	})
	t.Run("empty query is an error", func(t *testing.T) {
		_, err := v.Search(ctx, "  ", 5)
		assert.Err(t, err)
	})
}

func TestVisibilityExecuteUnloaded(t *testing.T) {
	ctx := context.Background()
	calls := 0
	v := NewVisibility(thirtyMCP(10, &calls), VisibilityOptions{Mode: DeferredAlways})
	var events []SearchEvent
	v.Bind(nil, func(_ context.Context, ev SearchEvent) { events = append(events, ev) })

	_, err := v.ExecuteTool(ctx, api.ToolCall{Name: "tool_05"})
	assert.Err(t, err)
	assert.StrContains(t, err.Error(), "not loaded yet")
	assert.StrContains(t, err.Error(), `"input_schema"`)
	assert.Eq(t, 0, calls) // inner layer untouched
	assert.Eq(t, 1, len(events))
	assert.Eq(t, SearchEventAutoLoad, events[0].Kind)
	assert.Eq(t, true, events[0].CacheInvalidated)

	got, _ := v.ListTools(ctx)
	assert.Eq(t, []string{"file_read", "update_plan", "tool_05"}, names(got))
	res, err := v.ExecuteTool(ctx, api.ToolCall{Name: "tool_05"})
	assert.NoErr(t, err)
	assert.Eq(t, "ran tool_05", res.Content)
	assert.Eq(t, 1, calls)

	// Direct tools and Lookup pass through.
	res, err = v.ExecuteTool(ctx, api.ToolCall{Name: "file_read"})
	assert.NoErr(t, err)
	assert.Eq(t, "r", res.Content)
	tool, err := v.Get("tool_09")
	assert.NoErr(t, err)
	assert.Eq(t, KindMCP, KindOf(tool))
}

func TestVisibilityPromptSection(t *testing.T) {
	reg := thirtyMCP(10, nil)
	reg.Register(&fakeMCP{name: "zz_long", server: "gofer", desc: "line one\n   " + strings.Repeat("y", 400)})
	v := NewVisibility(reg, VisibilityOptions{Mode: DeferredAlways})
	section := v.PromptSection()

	t.Run("grouped by server in sorted order", func(t *testing.T) {
		gofer := strings.Index(section, "### gofer")
		a := strings.Index(section, "### srv_a")
		b := strings.Index(section, "### srv_b")
		assert.Eq(t, true, gofer >= 0 && gofer < a && a < b)
		assert.Eq(t, true, strings.Index(section, "- gofer_issue_close") < strings.Index(section, "- gofer_issue_list"))
		assert.StrContains(t, section, "tool_search")
	})
	t.Run("description truncated to 160 characters", func(t *testing.T) {
		var line string
		for _, l := range strings.Split(section, "\n") {
			if strings.HasPrefix(l, "- zz_long — ") {
				line = strings.TrimPrefix(l, "- zz_long — ")
			}
		}
		assert.Eq(t, 160, len([]rune(line)))
		assert.Eq(t, true, strings.HasSuffix(line, "…"))
	})
	t.Run("stable after loading and empty when inactive", func(t *testing.T) {
		_, _ = v.Search(context.Background(), "select:tool_01", 5)
		assert.Eq(t, section, v.PromptSection())
		assert.Eq(t, "", NewVisibility(reg, VisibilityOptions{Mode: DeferredNever}).PromptSection())
	})
}

// memStore is an in-memory LoadedStore.
type memStore struct{ names []string }

func (m *memStore) LoadedTools(context.Context) ([]string, error)       { return m.names, nil }
func (m *memStore) SaveLoadedTools(_ context.Context, n []string) error { m.names = n; return nil }

func TestVisibilityBoundStore(t *testing.T) {
	ctx := context.Background()
	st := &memStore{names: []string{"tool_02", "removed_tool"}}
	v := NewVisibility(thirtyMCP(10, nil), VisibilityOptions{Mode: DeferredAlways})
	v.Bind(st, nil)
	got, _ := v.ListTools(ctx)
	assert.Eq(t, []string{"file_read", "update_plan", "tool_02"}, names(got)) // unknown names ignored
	_, _ = v.Search(ctx, "select:tool_01", 5)
	assert.Eq(t, []string{"tool_01", "tool_02"}, st.names)
}
