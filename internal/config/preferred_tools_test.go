package axconfig

import (
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
)

func TestReadPreferredTools(t *testing.T) {
	tests := []struct {
		name    string
		raw     any
		wantErr string
	}{
		{name: "valid list", raw: []map[string]any{{"name": " rg ", "kind": "COMMAND", "when": " search "}}},
		{name: "not a list", raw: map[string]any{"name": "rg"}, wantErr: "list of mappings"},
		{name: "unknown field", raw: []map[string]any{{"name": "rg", "kind": "command", "when": "search", "extra": true}}, wantErr: "list of mappings"},
		{name: "empty name", raw: []map[string]any{{"name": " ", "kind": "command", "when": "search"}}, wantErr: ".name"},
		{name: "name has arguments", raw: []map[string]any{{"name": "rg --json", "kind": "command", "when": "search"}}, wantErr: "without arguments"},
		{name: "empty when", raw: []map[string]any{{"name": "rg", "kind": "command", "when": " "}}, wantErr: ".when"},
		{name: "bad kind", raw: []map[string]any{{"name": "rg", "kind": "other", "when": "search"}}, wantErr: "command or mcp"},
		{name: "duplicate", raw: []map[string]any{{"name": "rg", "kind": "command", "when": "one"}, {"name": "rg", "kind": "command", "when": "two"}}, wantErr: "duplicate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gkconfig.Reset()
			t.Cleanup(gkconfig.Reset)
			assert.NoErr(t, gkconfig.Set("tools.preferred", tt.raw))
			got, err := ReadPreferredTools()
			if tt.wantErr != "" {
				assert.Err(t, err)
				assert.StrContains(t, err.Error(), tt.wantErr)
				return
			}
			assert.NoErr(t, err)
			assert.Eq(t, []PreferredTool{{Name: "rg", Kind: "command", When: "search"}}, got)
		})
	}
}

func TestPreferredToolRegistryAndEffectiveValue(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	assert.NoErr(t, gkconfig.Set("tools.preferred", []map[string]any{{"name": "rg", "kind": "command", "when": "search"}}))

	descriptor, err := MatchConfigKey("tools.preferred")
	assert.NoErr(t, err)
	assert.Eq(t, ConfigTypeYAML, descriptor.Type)
	assert.Eq(t, "preferred_tools", descriptor.Reader)
	value, err := ReadConfigValue("tools.preferred")
	assert.NoErr(t, err)
	assert.Eq(t, SourceConfig, value.Source)
	assert.Eq(t, []PreferredTool{{Name: "rg", Kind: "command", When: "search"}}, value.Value)
}
