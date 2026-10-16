package native

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/pkg/agentcore/api"
)

func TestFileRemoveSnapshotBeforeFailure(t *testing.T) {
	ws, tool := removeFixture(t)
	snapshots := &recordingSnapshotter{beforeErr: os.ErrPermission}
	tool.snapshots = snapshots
	_, err := tool.Execute(t.Context(), api.ToolCall{
		CallID:    "before-failure",
		Arguments: map[string]any{"path": "a.txt"},
	})
	assert.ErrMsgContains(t, err, "snapshot before")
	assert.Empty(t, snapshots.after)
	_, err = os.Stat(filepath.Join(ws, "a.txt"))
	assert.NoError(t, err)
}
