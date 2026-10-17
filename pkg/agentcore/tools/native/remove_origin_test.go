package native

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore/api"
)

func TestFileRemovePreservesOriginJSONPayload(t *testing.T) {
	ws, tool := removeFixture(t)
	path := filepath.Join(ws, "origin.json")
	assert.NoError(t, os.WriteFile(path, []byte("original bytes"), 0o640))
	_, err := tool.Execute(t.Context(), api.ToolCall{CallID: "origin-name", Arguments: map[string]any{"path": "origin.json"}})
	assert.Require(t, assert.NoError(t, err))

	entries, err := os.ReadDir(tool.trashDir)
	assert.Require(t, assert.NoError(t, err))
	assert.Require(t, assert.Len(t, entries, 1))
	entry := filepath.Join(tool.trashDir, entries[0].Name())
	content, err := os.ReadFile(filepath.Join(entry, ".suga-content"))
	assert.Require(t, assert.NoError(t, err))
	assert.Eq(t, "original bytes", string(content))
	origin, err := os.ReadFile(filepath.Join(entry, "origin.json"))
	assert.Require(t, assert.NoError(t, err))
	assert.True(t, strings.Contains(string(origin), `"name":".suga-content"`))
}
