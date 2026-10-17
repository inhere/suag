package axconfig

import (
	"os"
	"path/filepath"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
)

func TestReadSnapshotSettings(t *testing.T) {
	for name, body := range map[string]string{
		"defaults":   "model: openai/m\n",
		"configured": "tools:\n  snapshots:\n    enabled: false\n    max_file_bytes: 1024\n    max_session_bytes: 2048\n    max_global_bytes: 4096\n    keep_turns: 3\n    keep_days: 4\n    on_error: continue\n",
	} {
		t.Run(name, func(t *testing.T) {
			gkconfig.Reset()
			t.Cleanup(gkconfig.Reset)
			root := t.TempDir()
			t.Setenv("SUGA_HOME", root)
			t.Setenv("SUGA_CONFIG", "")
			t.Chdir(t.TempDir())
			assert.NoErr(t, os.WriteFile(filepath.Join(root, "config.yaml"), []byte(body), 0600))
			assert.NoErr(t, Load())
			got, err := ReadSnapshotSettings()
			assert.NoErr(t, err)
			if name == "defaults" {
				assert.True(t, got.Enabled)
				assert.Eq(t, DefaultSnapshotMaxGlobalBytes, got.MaxGlobalBytes)
				assert.Eq(t, SnapshotOnErrorDeny, got.OnError)
				return
			}
			assert.False(t, got.Enabled)
			assert.Eq(t, int64(1024), got.MaxFileBytes)
			assert.Eq(t, int64(2048), got.MaxSessionBytes)
			assert.Eq(t, int64(4096), got.MaxGlobalBytes)
			assert.Eq(t, 3, got.KeepTurns)
			assert.Eq(t, 4, got.KeepDays)
			assert.Eq(t, SnapshotOnErrorContinue, got.OnError)
		})
	}

	for name, body := range map[string]string{
		"wrong enabled": "tools:\n  snapshots:\n    enabled: yes\n",
		"zero limit":    "tools:\n  snapshots:\n    max_global_bytes: 0\n",
		"wrong error":   "tools:\n  snapshots:\n    on_error: maybe\n",
		"unknown field": "tools:\n  snapshots:\n    typo: true\n",
	} {
		t.Run(name, func(t *testing.T) {
			gkconfig.Reset()
			t.Cleanup(gkconfig.Reset)
			root := t.TempDir()
			t.Setenv("SUGA_HOME", root)
			t.Setenv("SUGA_CONFIG", "")
			t.Chdir(t.TempDir())
			assert.NoErr(t, os.WriteFile(filepath.Join(root, "config.yaml"), []byte(body), 0600))
			assert.Err(t, Load())
		})
	}
}

func TestChatUndoConfirmEnabled(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	assert.True(t, ChatUndoConfirmEnabled())
	assert.NoErr(t, gkconfig.Set("chat.undo_confirm", false))
	assert.False(t, ChatUndoConfirmEnabled())
	assert.NoErr(t, gkconfig.Set("chat.undo_confirm", "off"))
	assert.False(t, ChatUndoConfirmEnabled())
}
