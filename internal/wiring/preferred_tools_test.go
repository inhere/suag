package wiring

import (
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
	appconfig "github.com/inhere/suga/internal/config"
	"github.com/inhere/suga/pkg/agentcore/tools"
	toolsmcp "github.com/inhere/suga/pkg/agentcore/tools/mcp"
)

func TestResolvePreferredTools(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	assert.NoErr(t, gkconfig.Set("tools.preferred", []map[string]any{
		{"name": "go", "kind": "command", "when": "run Go"},
		{"name": "missing-preferred-command", "kind": "command", "when": "never"},
		{"name": "connected", "kind": "mcp", "when": "graph"},
		{"name": "offline", "kind": "mcp", "when": "other"},
		{"name": "unconfigured", "kind": "mcp", "when": "none"},
	}))

	registry := tools.NewRegistry()
	registry.Register(newServerStubTool("connected", "graph_search"))
	got, err := ResolvePreferredTools(registry, []toolsmcp.ServerSettings{
		{ID: "connected", Command: "server"},
		{ID: "offline", Command: "server"},
	})
	assert.NoErr(t, err)
	assert.Eq(t, []PreferredToolStatus{
		{Name: "go", Kind: "command", When: "run Go", Present: true, Reason: preferredReasonFoundPath},
		{Name: "missing-preferred-command", Kind: "command", When: "never", Reason: preferredReasonMissingPath},
		{Name: "connected", Kind: "mcp", When: "graph", Present: true, Reason: preferredReasonMCPConnected},
		{Name: "offline", Kind: "mcp", When: "other", Reason: preferredReasonMCPUnavailable},
		{Name: "unconfigured", Kind: "mcp", When: "none", Reason: preferredReasonMCPNotConfigured},
	}, got)

	preferred, readErr := appconfig.ReadPreferredTools()
	assert.NoError(t, readErr)
	assert.Eq(t, 5, len(preferred))
}

func TestRefreshPreferredCommands(t *testing.T) {
	got := refreshPreferredCommands([]PreferredToolStatus{
		{Name: "go", Kind: "command", Present: false, Reason: preferredReasonMissingPath},
		{Name: "missing-preferred-command", Kind: "command", Present: true, Reason: preferredReasonFoundPath},
		{Name: "server", Kind: "mcp", Present: true, Reason: preferredReasonMCPConnected},
	})
	assert.True(t, got[0].Present)
	assert.Eq(t, preferredReasonFoundPath, got[0].Reason)
	assert.False(t, got[1].Present)
	assert.Eq(t, preferredReasonMissingPath, got[1].Reason)
	assert.Eq(t, PreferredToolStatus{Name: "server", Kind: "mcp", Present: true, Reason: preferredReasonMCPConnected}, got[2])
}

func TestPreferredMCPToolNames(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(newServerStubTool("graph", "z_search"))
	registry.Register(newServerStubTool("graph", "a_lookup"))
	registry.Register(newServerStubTool("other", "other_tool"))
	got := preferredMCPToolNames(registry, []PreferredToolStatus{
		{Name: "graph", Kind: "mcp", Present: true},
		{Name: "other", Kind: "mcp", Present: false},
	})
	assert.Eq(t, []string{"a_lookup", "z_search"}, got)
}

func TestResolvePreferredToolsDoesNotExposeServerDetails(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	assert.NoError(t, gkconfig.Set("tools.preferred", []map[string]any{{"name": "offline", "kind": "mcp", "when": "graph"}}))
	got, err := ResolvePreferredTools(nil, []toolsmcp.ServerSettings{{ID: "offline", Command: "secret-server", Args: []string{"--token", "secret"}, Env: map[string]string{"TOKEN": "secret"}}})
	assert.NoError(t, err)
	assert.Eq(t, false, got[0].Present)
	assert.NotContains(t, got[0].Reason, "secret")
	assert.NotContains(t, got[0].Reason, "token")
}
