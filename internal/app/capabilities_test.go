package app

import (
	"context"
	"sort"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/testutil/assert"

	"github.com/inhere/axon/internal/capabilities/runtimecaps"
	axconfig "github.com/inhere/axon/internal/config"
	"github.com/inhere/axon/internal/platform/tools"
	toolsmcp "github.com/inhere/axon/internal/platform/tools/mcp"
	agentcoreapi "github.com/inhere/axon/pkg/agentcore/api"
)

func TestBuildDefaultCapabilities_RegistersNativeToolsAndCapabilitySet(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	workspace := t.TempDir()

	built, err := buildDefaultCapabilities(workspace)
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, built.Close())
	})

	assert.NotNil(t, built.Registry)
	assert.NotNil(t, built.Manager)

	assert.NotNil(t, mustGetTool(t, built.Registry, "file_read"))
	assert.NotNil(t, mustGetTool(t, built.Registry, "file_write"))
	assert.NotNil(t, mustGetTool(t, built.Registry, "file_list"))
	assert.NotNil(t, mustGetTool(t, built.Registry, "shell_exec"))

	set := built.Manager.BuildDefaultCapabilitySet(runtimecaps.BuildSetInput{})
	resolved := built.Manager.ResolveTurnCapabilities(set)
	assertToolNamesEq(t, []string{"file_read", "file_write", "file_list", "shell_exec"}, resolved.ToolNames)
}

func TestBuildDefaultCapabilities_RegistersWebSearchWhenConfigured(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	_ = gkconfig.Set("serpapi_key", "test-key")

	built, err := buildDefaultCapabilities(t.TempDir())
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, built.Close())
	})

	assert.NotNil(t, mustGetTool(t, built.Registry, "web_search"))

	set := built.Manager.BuildDefaultCapabilitySet(runtimecaps.BuildSetInput{})
	resolved := built.Manager.ResolveTurnCapabilities(set)
	assertToolNamesEq(t, []string{"file_read", "file_write", "file_list", "shell_exec", "web_search"}, resolved.ToolNames)
}

func TestBuildDefaultCapabilities_ResolvedRegistryMatchesCapabilityNames(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	built, err := buildDefaultCapabilities(t.TempDir())
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, built.Close())
	})

	set := built.Manager.BuildDefaultCapabilitySet(runtimecaps.BuildSetInput{})
	resolved := built.Manager.ResolveTurnCapabilities(set)
	subset := built.Registry.Subset(resolved.ToolNames)

	assertToolNamesEq(t, resolved.ToolNames, subset.List())
}

func mustGetTool(t *testing.T, registry *tools.Registry, name string) tools.Tool {
	t.Helper()

	tool, err := registry.Get(name)
	assert.NoErr(t, err)
	return tool
}

func TestBuildDefaultCapabilities_StaticProviderReturnsCopy(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	built, err := buildDefaultCapabilities(t.TempDir())
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, built.Close())
	})
	refs, err := built.Manager.BuildDefaultCapabilitySet(runtimecaps.BuildSetInput{}).Tools, error(nil)
	assert.NoErr(t, err)
	assert.True(t, len(refs) > 0)

	refs[0].Name = "mutated"

	resolved := built.Manager.ResolveTurnCapabilities(
		built.Manager.BuildDefaultCapabilitySet(runtimecaps.BuildSetInput{}),
	)
	assertToolNamesEq(t, []string{"file_read", "file_write", "file_list", "shell_exec"}, resolved.ToolNames)
}

func TestBuildDefaultCapabilities_RegistersConfiguredMCPTools(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	_ = gkconfig.Set("mcp.servers", []map[string]any{
		{
			"id":      "stub",
			"command": "stub",
		},
	})

	restore := swapMCPProviderFactoryForTest(func(_ context.Context, settings axconfig.MCPServerSettings) (capabilityToolProvider, error) {
		return toolsmcp.NewProviderForTools(
			settings.ID,
			[]tools.Tool{
				newCapabilityStubTool("mcp_echo"),
			},
			nil,
		), nil
	})
	t.Cleanup(restore)

	built, err := buildDefaultCapabilities(t.TempDir())
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, built.Close())
	})

	assert.NotNil(t, mustGetTool(t, built.Registry, "mcp_echo"))

	set := built.Manager.BuildDefaultCapabilitySet(runtimecaps.BuildSetInput{})
	resolved := built.Manager.ResolveTurnCapabilities(set)
	assertToolNamesEq(t, []string{"file_read", "file_write", "file_list", "shell_exec", "mcp_echo"}, resolved.ToolNames)
}

func assertToolNamesEq(t *testing.T, expected []string, actual []string) {
	t.Helper()

	expectedCopy := append([]string(nil), expected...)
	actualCopy := append([]string(nil), actual...)
	sort.Strings(expectedCopy)
	sort.Strings(actualCopy)

	assert.Eq(t, len(expectedCopy), len(actualCopy))
	for idx := range expectedCopy {
		assert.Eq(t, expectedCopy[idx], actualCopy[idx])
	}
}

type capabilityStubTool struct {
	name string
}

func newCapabilityStubTool(name string) tools.Tool {
	return &capabilityStubTool{name: name}
}

func (t *capabilityStubTool) Name() string        { return t.name }
func (t *capabilityStubTool) Description() string { return "capability stub tool" }
func (t *capabilityStubTool) Kind() tools.Kind    { return tools.KindMCP }
func (t *capabilityStubTool) Schema() agentcoreapi.ToolSchema {
	return agentcoreapi.ToolSchema{Name: t.name}
}
func (t *capabilityStubTool) Execute(_ context.Context, call agentcoreapi.ToolCall) (*agentcoreapi.ToolResult, error) {
	return &agentcoreapi.ToolResult{
		CallID:  call.CallID,
		Name:    call.Name,
		Content: t.name,
	}, nil
}
