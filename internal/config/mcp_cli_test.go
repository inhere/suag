package axconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestReadClaudeMCPConfigs(t *testing.T) {
	first := filepath.Join(t.TempDir(), "first.json")
	second := filepath.Join(t.TempDir(), "second.json")
	assert.NoErr(t, os.WriteFile(first, []byte(`{"mcpServers":{"alpha":{"command":"server-a","args":["--stdio"],"env":{"MODE":"test"}}}}`), 0600))
	assert.NoErr(t, os.WriteFile(second, []byte(`{"mcpServers":{"beta":{"command":"server-b"}}}`), 0600))
	servers, err := ReadClaudeMCPConfigs([]string{first, second})
	assert.NoErr(t, err)
	assert.Eq(t, 2, len(servers))
	assert.Eq(t, "alpha", servers[0].ID)
	assert.Eq(t, "--stdio", servers[0].Args[0])
	assert.Eq(t, "test", servers[0].Env["MODE"])
	assert.Eq(t, "beta", servers[1].ID)
}

func TestReadClaudeMCPConfigsRejectsNonStdioAndDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	for _, content := range []string{
		`{"mcpServers":{"remote":{"url":"https://example.test/mcp"}}}`,
		`{"mcpServers":{"remote":{"command":"server","type":"http"}}}`,
	} {
		assert.NoErr(t, os.WriteFile(path, []byte(content), 0600))
		_, err := ReadClaudeMCPConfigs([]string{path})
		assert.True(t, err != nil)
	}
	assert.NoErr(t, os.WriteFile(path, []byte(`{"mcpServers":{"same":{"command":"server"}}}`), 0600))
	_, err := ReadClaudeMCPConfigs([]string{path, path})
	assert.True(t, err != nil)
}
