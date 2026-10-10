package mcp

import (
	"context"
	"sort"
	"testing"

	"github.com/gookit/goutil/x/assert"

	axconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/pkg/agentcore/api"
)

func TestProvider_RegisterTools(t *testing.T) {
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

	read, err := registry.Get("fs_read")
	assert.NoErr(t, err)
	assert.Eq(t, tools.KindMCP, read.Kind())
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

	registry := tools.NewRegistry()
	assert.NoErr(t, provider.RegisterTools(registry))
	names := registry.List()
	sort.Strings(names)
	assert.Eq(t, []string{"fs_read", "fs_write"}, names)
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

// The approval tables (wiring, acpserver) use MCP stubs; this pins the real
// adapter to the same classification: MCP kind with its server name, even
// when it is named like a built-in tool.
func TestClientAdapterIsMCPTool(t *testing.T) {
	tool := NewClient(ServerConfig{Name: "srv"}).AsTool(mcpTool{Name: "file_read"})
	assert.Eq(t, tools.KindMCP, tool.Kind())
	named, ok := tool.(tools.MCPServerNamer)
	assert.Require(t, assert.True(t, ok))
	assert.Eq(t, "srv", named.MCPServer())
	assert.Eq(t, "file_read", tool.Name())
}
