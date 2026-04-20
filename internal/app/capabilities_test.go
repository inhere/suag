package app

import (
	"sort"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/testutil/assert"

	"github.com/inhere/axon/internal/capabilities/runtimecaps"
	"github.com/inhere/axon/internal/platform/tools"
)

func TestBuildDefaultCapabilities_RegistersNativeToolsAndCapabilitySet(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	workspace := t.TempDir()

	built := buildDefaultCapabilities(workspace)

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

	built := buildDefaultCapabilities(t.TempDir())

	assert.NotNil(t, mustGetTool(t, built.Registry, "web_search"))

	set := built.Manager.BuildDefaultCapabilitySet(runtimecaps.BuildSetInput{})
	resolved := built.Manager.ResolveTurnCapabilities(set)
	assertToolNamesEq(t, []string{"file_read", "file_write", "file_list", "shell_exec", "web_search"}, resolved.ToolNames)
}

func TestBuildDefaultCapabilities_ResolvedRegistryMatchesCapabilityNames(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	built := buildDefaultCapabilities(t.TempDir())

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

	built := buildDefaultCapabilities(t.TempDir())

	refs, err := built.Manager.BuildDefaultCapabilitySet(runtimecaps.BuildSetInput{}).Tools, error(nil)
	assert.NoErr(t, err)
	assert.True(t, len(refs) > 0)

	refs[0].Name = "mutated"

	resolved := built.Manager.ResolveTurnCapabilities(
		built.Manager.BuildDefaultCapabilitySet(runtimecaps.BuildSetInput{}),
	)
	assertToolNamesEq(t, []string{"file_read", "file_write", "file_list", "shell_exec"}, resolved.ToolNames)
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
