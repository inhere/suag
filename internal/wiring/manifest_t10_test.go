package wiring

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
	appconfig "github.com/inhere/suga/internal/config"
	"github.com/inhere/suga/pkg/agentcore/api"
)

func TestManifestApprovalRulesDiagnostics(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SUGA_HOME", home)
	workspace := manifestConfig(t)
	projectPath := filepath.Join(workspace, ".suga", appconfig.ApprovalRulesFileName)
	assert.Require(t, assert.NoErr(t, os.MkdirAll(filepath.Dir(projectPath), 0o700)))
	text := "rules:\n  - tool: file_write\n    action: allow\n  - tool: file_remove\n    action: deny\n"
	assert.Require(t, assert.NoErr(t, os.WriteFile(projectPath, []byte(text), 0o600)))

	m, err := LoadManifest(ManifestOptions{Workspace: workspace})
	assert.Require(t, assert.NoErr(t, err))
	assert.True(t, m.ApprovalRules.Enabled)
	assert.Eq(t, filepath.Join(home, appconfig.ApprovalRulesFileName), m.ApprovalRules.User.Path)
	assert.Eq(t, projectPath, m.ApprovalRules.Project.Path)
	assert.NotEmpty(t, m.ApprovalRules.Project.Hash)
	assert.False(t, m.ApprovalRules.Project.Trusted)
	assert.StrContains(t, m.ApprovalRules.Project.TrustReason, "no trusted hash")
	assert.Eq(t, 1, m.ApprovalRules.Project.EffectiveRules)
	assert.Require(t, assert.Len(t, m.ApprovalRules.Project.Ignored, 1))
	assert.Eq(t, 1, m.ApprovalRules.Project.Ignored[0].Index)
	assert.StrContains(t, m.ApprovalRules.Project.Ignored[0].Reason, "no trusted hash")
	assert.Eq(t, "ask", m.ApprovalRules.Defaults.ToolsApproval)
}

func TestManifestApprovalRulesStaticAndRuntimeShareSnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SUGA_HOME", home)
	workspace := manifestConfig(t)
	assert.NoErr(t, gkconfig.Set("mcp.servers", nil))
	assert.NoErr(t, gkconfig.Set("tools.approval", "ask"))

	static, err := LoadManifest(ManifestOptions{Workspace: workspace})
	assert.Require(t, assert.NoErr(t, err))
	rt, err := OpenCLI(context.Background(), Options{Workspace: workspace, ChatMode: true, AllowWrite: true})
	assert.Require(t, assert.NoErr(t, err))
	t.Cleanup(func() { _ = rt.Close() })
	result, err := rt.Registry.ExecuteTool(context.Background(), api.ToolCall{CallID: "manifest", Name: "suga_manifest"})
	assert.Require(t, assert.NoErr(t, err))
	var live Manifest
	assert.Require(t, assert.NoErr(t, json.Unmarshal([]byte(result.Content), &live)))
	assert.Eq(t, static.ApprovalRules, live.ApprovalRules)
}
