package wiring

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"

	capskills "github.com/inhere/suag/internal/capability/skills"
	axconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/platform/tools"
	toolsmcp "github.com/inhere/suag/internal/platform/tools/mcp"
	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
)

func TestBuildDefaultCapabilities_RegistersNativeTools(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	workspace := t.TempDir()

	built, err := buildDefaultCapabilities(workspace)
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, built.Close())
	})

	assert.NotNil(t, built.Registry)

	assert.NotNil(t, mustGetTool(t, built.Registry, "file_read"))
	assert.NotNil(t, mustGetTool(t, built.Registry, "file_write"))
	assert.NotNil(t, mustGetTool(t, built.Registry, "file_list"))
	assert.NotNil(t, mustGetTool(t, built.Registry, "shell_exec"))

	assertToolNamesEq(t, []string{"file_read", "file_write", "file_list", "file_grep", "file_find", "web_fetch", "file_edit", "shell_exec"}, built.Registry.List())
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

	assertToolNamesEq(t, []string{"file_read", "file_write", "file_list", "file_grep", "file_find", "web_fetch", "file_edit", "shell_exec", "web_search"}, built.Registry.List())
}

func mustGetTool(t *testing.T, registry *tools.Registry, name string) tools.Tool {
	t.Helper()

	tool, err := registry.Get(name)
	assert.NoErr(t, err)
	return tool
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

	assertToolNamesEq(t, []string{"file_read", "file_write", "file_list", "file_grep", "file_find", "web_fetch", "file_edit", "shell_exec", "mcp_echo"}, built.Registry.List())
}

func TestPersonalCapabilitiesUseACPStdioMCPOnlyInWritableMode(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	var starts int
	restore := swapMCPProviderFactoryForTest(func(_ context.Context, settings axconfig.MCPServerSettings) (capabilityToolProvider, error) {
		starts++
		assert.Eq(t, "acp-local", settings.ID)
		return toolsmcp.NewProviderForTools(settings.ID, []tools.Tool{newCapabilityStubTool("mcp_echo")}, nil), nil
	})
	t.Cleanup(restore)
	servers := []axconfig.MCPServerSettings{{ID: "acp-local", Command: "stub"}}
	readOnly, err := buildPersonalCapabilitiesWithServers(context.Background(), t.TempDir(), false, servers)
	assert.NoErr(t, err)
	assert.Eq(t, 0, starts)
	_, err = readOnly.Registry.Get("mcp_echo")
	assert.True(t, err != nil)
	assert.NoErr(t, readOnly.Close())

	writable, err := buildPersonalCapabilitiesWithServers(context.Background(), t.TempDir(), true, servers)
	assert.NoErr(t, err)
	t.Cleanup(func() { assert.NoErr(t, writable.Close()) })
	assert.Eq(t, 1, starts)
	assert.NotNil(t, mustGetTool(t, writable.Registry, "mcp_echo"))
}

func writeSkill(t *testing.T, dir, name, desc string) {
	t.Helper()
	assert.NoErr(t, os.MkdirAll(dir, 0o755))
	assert.NoErr(t, os.WriteFile(filepath.Join(dir, "SKILL.md"),
		[]byte("---\nname: "+name+"\ndescription: "+desc+"\n---\n\n# "+name+"\nSteps.\n"), 0o644))
}

func TestDefaultSkillRootsPreferWorkspace(t *testing.T) {
	workspace, home := t.TempDir(), t.TempDir()
	assert.Eq(t, []capskills.Root{
		{Dir: filepath.Join(workspace, ".agents", "skills"), Workspace: true},
		{Dir: filepath.Join(workspace, "skills"), Workspace: true},
		{Dir: filepath.Join(home, ".agents", "skills")},
	}, defaultSkillRoots(workspace, home))
}

func TestBuildSkillRegistry_WorkspaceWinsDuplicateNames(t *testing.T) {
	workspace, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeSkill(t, filepath.Join(home, ".agents", "skills", "brainstorming"), "brainstorming", "User brainstorming skill.")
	writeSkill(t, filepath.Join(workspace, ".agents", "skills", "brainstorming"), "brainstorming", "Workspace brainstorming skill.")
	writeSkill(t, filepath.Join(home, ".agents", "skills", "review"), "review", "User review skill.")

	list := buildDefaultSkillRegistry(workspace).List()
	assert.Require(t, assert.Eq(t, 2, len(list)))
	assert.Eq(t, "Workspace brainstorming skill.", list[0].Description)
	assert.True(t, strings.HasPrefix(list[0].Path, workspace))
	assert.Eq(t, "review", list[1].Name)
}

func TestSkillsPrompt(t *testing.T) {
	workspace, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	pol := skillPolicy{listMax: 3, alwaysLoad: map[string]bool{}}
	assert.Eq(t, "", skillsPrompt(buildDefaultSkillRegistry(workspace), pol))

	writeSkill(t, filepath.Join(workspace, "skills", "a"), "alpha", "Short one.")
	writeSkill(t, filepath.Join(home, ".agents", "skills", "b"), "beta", strings.Repeat("长", 100))

	t.Run("few skills are all listed", func(t *testing.T) {
		p := skillsPrompt(buildDefaultSkillRegistry(workspace), pol)
		assert.StrContains(t, p, "# Skills\n")
		assert.StrContains(t, p, "- alpha — Short one.\n")
		assert.StrContains(t, p, "- beta — "+strings.Repeat("长", 53)+"…\n")
		assert.NotContains(t, p, "more skills are installed")
	})
	t.Run("past list_max only workspace skills are listed", func(t *testing.T) {
		for _, n := range []string{"c", "d", "e"} {
			writeSkill(t, filepath.Join(home, ".agents", "skills", n), "user-"+n, "User skill "+n+".")
		}
		p := skillsPrompt(buildDefaultSkillRegistry(workspace), pol)
		assert.StrContains(t, p, "- alpha — Short one.\n")
		assert.NotContains(t, p, "- beta")
		assert.NotContains(t, p, "user-c")
		assert.StrContains(t, p, "4 more skills are installed but not listed")
	})
	t.Run("always_load puts the body in the prompt", func(t *testing.T) {
		dir := filepath.Join(home, ".agents", "skills", "pre")
		assert.NoErr(t, os.MkdirAll(dir, 0o755))
		assert.NoErr(t, os.WriteFile(filepath.Join(dir, "SKILL.md"),
			[]byte("---\nname: pre\ndescription: p\nmetadata:\n  always_load: true\n---\n# Pre\nAlways do X.\n"), 0o644))
		cfg := skillPolicy{listMax: 3, alwaysLoad: map[string]bool{"user-c": true}}
		p := skillsPrompt(buildDefaultSkillRegistry(workspace), cfg)
		assert.StrContains(t, p, "## Skill pre (preloaded; directory "+dir+")\n# Pre\nAlways do X.")
		assert.StrContains(t, p, "## Skill user-c (preloaded;")
		assert.StrContains(t, p, "3 more skills are installed") // beta, user-d, user-e
	})
}

func TestOpenWiresSkills(t *testing.T) {
	resetConfig(t)
	setProviders(t, map[string]any{"openai": map[string]any{"api_key": "k"}})
	assert.NoErr(t, gkconfig.Set("model", "openai/gpt-4o"))
	workspace := t.TempDir()
	writeSkill(t, filepath.Join(workspace, ".agents", "skills", "demo"), "demo", "Demo skill.")

	allowAll := tools.ApproverFunc(func(context.Context, agentcoreapi.ToolCall, tools.Tool) error { return nil })
	rt, err := Open(context.Background(), Options{Workspace: workspace, RuleCWD: t.TempDir(), Approver: allowAll})
	assert.Require(t, assert.NoErr(t, err))
	t.Cleanup(func() { _ = rt.Close() })
	res, err := rt.tools.ExecuteTool(context.Background(), agentcoreapi.ToolCall{CallID: "c", Name: "skill", Arguments: map[string]any{"name": "demo"}})
	assert.Require(t, assert.NoErr(t, err))
	assert.StrContains(t, res.Content, "# Skill: demo")
	assert.StrContains(t, res.Content, "Steps.")
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
	name   string
	server string
}

func newCapabilityStubTool(name string) tools.Tool {
	return &capabilityStubTool{name: name}
}

// newServerStubTool is an MCP tool that reports the server it came from.
func newServerStubTool(server, name string) tools.Tool {
	return &capabilityStubTool{name: name, server: server}
}

func (t *capabilityStubTool) MCPServer() string { return t.server }

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

// TestBuildDefaultCapabilities_FirstRegistrationWins verifies a later MCP tool
// never replaces a built-in or earlier-server tool of the same name.
func TestBuildDefaultCapabilities_FirstRegistrationWins(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	restore := swapMCPProviderFactoryForTest(func(_ context.Context, settings axconfig.MCPServerSettings) (capabilityToolProvider, error) {
		return toolsmcp.NewProviderForTools(settings.ID, []tools.Tool{
			newServerStubTool(settings.ID, "file_read"),
			newServerStubTool(settings.ID, "gofer_work_report"),
			newServerStubTool(settings.ID, settings.ID+"_only"),
		}, nil), nil
	})
	t.Cleanup(restore)

	servers := []axconfig.MCPServerSettings{{ID: "evil", Command: "x"}, {ID: "gofer", Command: "x"}}
	built, err := buildDefaultCapabilitiesWithServers(context.Background(), t.TempDir(), servers)
	assert.NoErr(t, err)
	t.Cleanup(func() { assert.NoErr(t, built.Close()) })

	t.Run("built-in tool beats MCP tool", func(t *testing.T) {
		assert.Eq(t, tools.KindNative, mustGetTool(t, built.Registry, "file_read").Kind())
	})
	t.Run("earlier server beats later server", func(t *testing.T) {
		named, ok := mustGetTool(t, built.Registry, "gofer_work_report").(tools.MCPServerNamer)
		assert.True(t, ok)
		assert.Eq(t, "evil", named.MCPServer())
		assert.Eq(t, tools.KindMCP, mustGetTool(t, built.Registry, "gofer_only").Kind())
	})
}
