package mcp

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"

	axconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/pkg/agentcore/api"
)

func TestProvider_RegisterToolsAndListToolRefs(t *testing.T) {
	registry := tools.NewRegistry()
	provider := &Provider{
		id: "mcp.filesystem",
		tools: []tools.Tool{
			newStubMCPTool("fs_read"),
			newStubMCPTool("fs_write"),
		},
	}

	err := provider.RegisterTools(registry)
	assert.NoErr(t, err)

	refs, err := provider.ListToolRefs(context.Background())
	assert.NoErr(t, err)
	assert.Eq(t, 2, len(refs))
	assert.Eq(t, "fs_read", refs[0].Name)
	assert.Eq(t, string(tools.KindMCP), refs[0].Source)
	assert.Eq(t, "mcp.filesystem", refs[0].ProviderID)
	assert.Eq(t, "fs_write", refs[1].Name)

	_, err = registry.Get("fs_read")
	assert.NoErr(t, err)
	_, err = registry.Get("fs_write")
	assert.NoErr(t, err)
}

func TestProvider_CloseStopsUnderlyingClient(t *testing.T) {
	client := &stubClosableClient{}
	provider := &Provider{
		id:     "mcp.filesystem",
		client: client,
	}

	assert.NoErr(t, provider.Close())
	assert.Eq(t, 1, client.stopCalls)
}

func TestNewProvider_StartsClientAndLoadsTools(t *testing.T) {
	restore := swapNewClientForTest(func(cfg ServerConfig) toolClient {
		assert.Eq(t, "filesystem", cfg.Name)
		assert.Eq(t, "npx", cfg.Command)
		assert.Eq(t, 2, len(cfg.Args))
		return &stubToolClient{
			tools: []mcpTool{
				{Name: "fs_read"},
				{Name: "fs_write"},
			},
		}
	})
	t.Cleanup(restore)

	provider, err := NewProvider(context.Background(), axconfig.MCPServerSettings{
		ID:      "filesystem",
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-filesystem"},
		Env: map[string]string{
			"NODE_ENV": "development",
		},
	})
	assert.NoErr(t, err)
	assert.NotNil(t, provider)
	t.Cleanup(func() {
		assert.NoErr(t, provider.Close())
	})

	assert.Eq(t, "mcp.filesystem", provider.ID())

	refs, err := provider.ListToolRefs(context.Background())
	assert.NoErr(t, err)
	assert.Eq(t, 2, len(refs))
	assert.Eq(t, "fs_read", refs[0].Name)
	assert.Eq(t, "fs_write", refs[1].Name)
}

func swapNewClientForTest(factory func(cfg ServerConfig) toolClient) func() {
	prev := newClient
	newClient = factory
	return func() {
		newClient = prev
	}
}

type stubToolClient struct {
	tools     []mcpTool
	started   bool
	stopCalls int
}

func (c *stubToolClient) Start(ctx context.Context) error {
	_ = ctx
	c.started = true
	return nil
}

func (c *stubToolClient) ListTools(ctx context.Context) ([]mcpTool, error) {
	_ = ctx
	return append([]mcpTool(nil), c.tools...), nil
}

func (c *stubToolClient) AsTool(t mcpTool) tools.Tool {
	return newStubMCPTool(t.Name)
}

func (c *stubToolClient) Stop() {
	c.stopCalls++
}

type stubMCPTool struct {
	name string
}

func newStubMCPTool(name string) tools.Tool {
	return &stubMCPTool{name: name}
}

func (t *stubMCPTool) Name() string        { return t.name }
func (t *stubMCPTool) Description() string { return "stub mcp tool" }
func (t *stubMCPTool) Kind() tools.Kind    { return tools.KindMCP }
func (t *stubMCPTool) Schema() api.ToolSchema {
	return api.ToolSchema{Name: t.name}
}
func (t *stubMCPTool) Execute(_ context.Context, call api.ToolCall) (*api.ToolResult, error) {
	return &api.ToolResult{
		CallID:  call.CallID,
		Name:    call.Name,
		Content: t.name,
	}, nil
}

type stubClosableClient struct {
	stopCalls int
}

func (c *stubClosableClient) Start(ctx context.Context) error {
	_ = ctx
	return nil
}

func (c *stubClosableClient) ListTools(ctx context.Context) ([]mcpTool, error) {
	_ = ctx
	return nil, nil
}

func (c *stubClosableClient) AsTool(t mcpTool) tools.Tool {
	return newStubMCPTool(t.Name)
}

func (c *stubClosableClient) Stop() {
	c.stopCalls++
}

func TestProvider_RegisterToolsKeepsFirstRegistration(t *testing.T) {
	registry := tools.NewRegistry()
	first := &stubMCPTool{name: "dup"}
	registry.Register(first)
	p := NewProviderForTools("evil", []tools.Tool{&stubMCPTool{name: "dup"}, &stubMCPTool{name: "fresh"}}, nil)
	assert.NoErr(t, p.RegisterTools(registry))

	got, err := registry.Get("dup")
	assert.NoErr(t, err)
	assert.True(t, got == tools.Tool(first))
	_, err = registry.Get("fresh")
	assert.NoErr(t, err)
}
