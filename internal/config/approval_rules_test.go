package axconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
)

func TestReadApprovalRulesSettings(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   any
		want    ApprovalRulesSettings
		wantErr string
	}{
		{name: "defaults", want: DefaultApprovalRulesSettings()},
		{
			name: "configured options",
			value: map[string]any{
				"enabled":              false,
				"always_prefix_tokens": 4,
				"persist_ask":          false,
				"project_trust":        map[string]any{"C:\\repo": strings.Repeat("a", 64)},
			},
			want: ApprovalRulesSettings{
				Enabled:            false,
				AlwaysPrefixTokens: 4,
				PersistAsk:         false,
				ProjectTrust:       map[string]string{filepath.Clean(`C:\repo`): strings.Repeat("a", 64)},
			},
		},
		{name: "unknown field", value: map[string]any{"rules": []any{}}, wantErr: "not a recognized"},
		{name: "prefix must be positive", value: map[string]any{"always_prefix_tokens": 0}, wantErr: ">= 1"},
		{name: "project trust requires a hash", value: map[string]any{"project_trust": map[string]any{"C:\\repo": "short"}}, wantErr: "SHA-256"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gkconfig.Reset()
			t.Cleanup(gkconfig.Reset)
			if tc.value != nil {
				assert.NoErr(t, gkconfig.Set("tools.approval_rules", tc.value))
			}
			got, err := ReadApprovalRulesSettings()
			if tc.wantErr != "" {
				assert.Err(t, err)
				assert.StrContains(t, err.Error(), tc.wantErr)
				return
			}
			assert.NoErr(t, err)
			assert.Eq(t, tc.want.Enabled, got.Enabled)
			assert.Eq(t, tc.want.AlwaysPrefixTokens, got.AlwaysPrefixTokens)
			assert.Eq(t, tc.want.PersistAsk, got.PersistAsk)
			assert.Eq(t, tc.want.ProjectTrust, got.ProjectTrust)
		})
	}
}

func TestLoadRejectsInvalidApprovalRulesSettings(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	root := t.TempDir()
	t.Setenv("SUGA_HOME", root)
	t.Setenv("SUGA_CONFIG", "")
	t.Chdir(t.TempDir())
	config := "model: openai/test\ntools:\n  approval_rules:\n    always_prefix_tokens: 0\n"
	assert.NoErr(t, os.WriteFile(filepath.Join(root, "config.yaml"), []byte(config), 0600))
	assert.Err(t, Load())
}

func TestApprovalRulesPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SUGA_HOME", home)
	workspace := t.TempDir()

	user, err := ResolveUserApprovalRulesPath()
	assert.NoErr(t, err)
	assert.Eq(t, filepath.Join(home, ApprovalRulesFileName), user)
	project, err := ResolveProjectApprovalRulesPath(workspace)
	assert.NoErr(t, err)
	assert.Eq(t, filepath.Join(workspace, ".suga", ApprovalRulesFileName), project)
}
