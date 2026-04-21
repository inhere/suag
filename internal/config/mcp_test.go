package axconfig

import (
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/testutil/assert"
)

func TestReadMCPServerSettings(t *testing.T) {
	t.Run("returns empty when config missing", func(t *testing.T) {
		gkconfig.Reset()
		t.Cleanup(gkconfig.Reset)

		servers := ReadMCPServerSettings()
		assert.Eq(t, 0, len(servers))
	})

	t.Run("returns normalized servers", func(t *testing.T) {
		gkconfig.Reset()
		t.Cleanup(gkconfig.Reset)

		_ = gkconfig.Set("mcp.servers", []map[string]any{
			{
				"id":      " filesystem ",
				"command": " npx ",
				"args":    []string{" -y ", " @modelcontextprotocol/server-filesystem ", " ./workspace "},
				"env": map[string]any{
					"NODE_ENV": " development ",
					"EMPTY":    " ",
				},
			},
			{
				"id":      "ignored",
				"command": " ",
			},
			{
				"command": " uvx ",
				"args":    []any{" example-mcp "},
			},
		})

		servers := ReadMCPServerSettings()

		assert.Eq(t, 2, len(servers))
		assert.Eq(t, "filesystem", servers[0].ID)
		assert.Eq(t, "npx", servers[0].Command)
		assert.Eq(t, 3, len(servers[0].Args))
		assert.Eq(t, "-y", servers[0].Args[0])
		assert.Eq(t, "@modelcontextprotocol/server-filesystem", servers[0].Args[1])
		assert.Eq(t, "./workspace", servers[0].Args[2])
		assert.Eq(t, "development", servers[0].Env["NODE_ENV"])
		assert.Eq(t, "", servers[0].Env["EMPTY"])

		assert.Eq(t, "uvx", servers[1].ID)
		assert.Eq(t, "uvx", servers[1].Command)
		assert.Eq(t, 1, len(servers[1].Args))
		assert.Eq(t, "example-mcp", servers[1].Args[0])
	})
}
