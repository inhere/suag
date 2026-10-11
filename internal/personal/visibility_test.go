package personal

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/api"
	"github.com/inhere/suag/pkg/agentcore/tools"
	"github.com/inhere/suag/pkg/agentcore/tools/native"
)

// vMCP is a minimal MCP-kind tool.
type vMCP struct{ name, server string }

func (m vMCP) Name() string        { return m.name }
func (m vMCP) Description() string { return "tool " + m.name + " for " + m.server }
func (m vMCP) Kind() tools.Kind    { return tools.KindMCP }
func (m vMCP) MCPServer() string   { return m.server }
func (m vMCP) Schema() api.ToolSchema {
	return api.ToolSchema{Name: m.name, Description: m.Description(), InputSchema: map[string]any{"type": "object"}}
}
func (m vMCP) Execute(_ context.Context, call api.ToolCall) (*api.ToolResult, error) {
	return &api.ToolResult{CallID: call.CallID, Name: m.name, Content: "ran " + m.name}, nil
}

// approvalCounter stands in for the approval layer: it counts MCP executions.
type approvalCounter struct {
	reg *tools.Registry
	mu  sync.Mutex
	n   map[string]int
}

func (a *approvalCounter) ListTools(ctx context.Context) ([]agentcore.ToolSchema, error) {
	return a.reg.ListTools(ctx)
}
func (a *approvalCounter) Get(name string) (api.Tool, error) { return a.reg.Get(name) }
func (a *approvalCounter) ExecuteTool(ctx context.Context, call agentcore.ToolCall) (*agentcore.ToolResult, error) {
	a.mu.Lock()
	a.n[call.Name]++
	a.mu.Unlock()
	return a.reg.ExecuteTool(ctx, call)
}

// reqProv replays scripted steps and records each request.
type reqProv struct {
	mu    sync.Mutex
	steps []*agentcore.GenerateResult
	reqs  []*agentcore.GenerateRequest
}

func (p *reqProv) Name() string { return "req" }
func (p *reqProv) Generate(_ context.Context, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reqs = append(p.reqs, req)
	i := len(p.reqs) - 1
	if i >= len(p.steps) {
		return &agentcore.GenerateResult{Content: "done", FinishReason: agentcore.FinishFinal}, nil
	}
	return p.steps[i], nil
}

func callStep(id, name string, args map[string]any) *agentcore.GenerateResult {
	return &agentcore.GenerateResult{
		ToolCalls:    []agentcore.ToolCall{{CallID: id, Name: name, Arguments: args}},
		FinishReason: agentcore.FinishToolCalls,
	}
}

func toolNames(req *agentcore.GenerateRequest) []string {
	var out []string
	for _, s := range req.Tools {
		out = append(out, s.Name)
	}
	return out
}

// visRunner builds a Runner over 30 MCP tools in deferred mode, wired as
// observe(in New) -> visibility -> approval counter -> registry.
func visRunner(t *testing.T, repo *store.Store, prov agentcore.Provider) (*Runner, *approvalCounter) {
	t.Helper()
	reg := tools.NewRegistry()
	reg.Register(fnTool{name: "file_read", fn: func() (*agentcore.ToolResult, error) { return &agentcore.ToolResult{Content: "r"}, nil }})
	for i := 0; i < 30; i++ {
		server := []string{"beta", "alpha"}[i%2]
		reg.Register(vMCP{name: fmt.Sprintf("mcp_%02d", i), server: server})
	}
	gate := &approvalCounter{reg: reg, n: map[string]int{}}
	vis := tools.NewVisibility(gate, tools.VisibilityOptions{Mode: tools.DeferredAlways})
	reg.Register(native.NewToolSearchTool(vis))
	runner, err := New(Config{Repository: repo, Provider: prov, Tools: vis, Visibility: vis})
	assert.NoErr(t, err)
	return runner, gate
}

func TestDeferredToolsSearchLoadAndResume(t *testing.T) {
	ctx := context.Background()
	repo := openObserveRepo(t)
	prov := &reqProv{steps: []*agentcore.GenerateResult{
		callStep("c1", "tool_search", map[string]any{"query": "select:mcp_05,mcp_02"}),
		callStep("c2", "mcp_05", nil),
		{Content: "first done", FinishReason: agentcore.FinishFinal},
	}}
	runner, gate := visRunner(t, repo, prov)
	res, err := runner.Run(ctx, RunRequest{Prompt: "go"})
	assert.NoErr(t, err)
	sid := res.SessionID

	t.Run("segment context lists deferred tools by server", func(t *testing.T) {
		// The listing moved from the system prompt into the segment context.
		assert.NotContains(t, prov.reqs[0].System, "### alpha")
		assert.Require(t, assert.True(t, isSegmentContext(prov.reqs[0].Messages[0])))
		sys := prov.reqs[0].Messages[0].Content
		assert.StrContains(t, sys, "### alpha")
		assert.Eq(t, true, strings.Index(sys, "### alpha") < strings.Index(sys, "### beta"))
		assert.StrContains(t, sys, "- mcp_00 — ")
	})
	t.Run("loaded tools appear after direct ones from the next step", func(t *testing.T) {
		assert.Eq(t, []string{"file_read", "tool_search"}, toolNames(prov.reqs[0]))
		assert.Eq(t, []string{"file_read", "tool_search", "mcp_02", "mcp_05"}, toolNames(prov.reqs[1]))
	})
	t.Run("loaded set is persisted and executes through approval", func(t *testing.T) {
		sess, err := repo.GetSession(ctx, sid)
		assert.NoErr(t, err)
		assert.Eq(t, []string{"mcp_02", "mcp_05"}, loadedToolNames(sess.Meta))
		assert.Eq(t, 1, gate.n["mcp_05"])
	})
	t.Run("tool_search event", func(t *testing.T) {
		evs := eventsOf(t, repo, sid, store.EventToolSearch)
		assert.Eq(t, 1, len(evs))
		assert.Eq(t, "tool_search", evs[0].Name)
		assert.Eq(t, "select:mcp_05,mcp_02", evs[0].Attrs["query"])
		assert.Eq(t, true, evs[0].Attrs["cache_invalidated"])
	})

	t.Run("resume with a new runner keeps the loaded set", func(t *testing.T) {
		prov2 := &reqProv{}
		runner2, _ := visRunner(t, repo, prov2)
		_, err := runner2.Run(ctx, RunRequest{SessionID: sid, Prompt: "again"})
		assert.NoErr(t, err)
		assert.Eq(t, []string{"file_read", "tool_search", "mcp_02", "mcp_05"}, toolNames(prov2.reqs[0]))
	})
}

func TestDeferredToolDirectCallAutoLoads(t *testing.T) {
	ctx := context.Background()
	repo := openObserveRepo(t)
	prov := &reqProv{steps: []*agentcore.GenerateResult{
		callStep("c1", "mcp_07", nil),
		callStep("c2", "mcp_07", nil),
		{Content: "ok", FinishReason: agentcore.FinishFinal},
	}}
	runner, gate := visRunner(t, repo, prov)
	var results []string
	_, err := runner.Run(ctx, RunRequest{Prompt: "go", Emit: func(_ context.Context, e Event) error {
		if e.Kind == "tool_result" {
			results = append(results, e.Text)
		}
		return nil
	}})
	assert.NoErr(t, err)
	assert.Eq(t, 2, len(results))
	assert.StrContains(t, results[0], "not loaded yet")
	assert.StrContains(t, results[0], "input_schema")
	assert.Eq(t, "ran mcp_07", results[1])
	// Only the second (loaded) call reached the approval layer.
	assert.Eq(t, 1, gate.n["mcp_07"])
	assert.Eq(t, []string{"file_read", "tool_search", "mcp_07"}, toolNames(prov.reqs[1]))

	sessions, err := repo.ListSessions(ctx)
	assert.NoErr(t, err)
	evs := eventsOf(t, repo, sessions[0].ID, store.EventToolSearch)
	assert.Eq(t, 1, len(evs))
	assert.Eq(t, "auto_load", evs[0].Name)
	assert.Eq(t, true, evs[0].Attrs["cache_invalidated"])
}
