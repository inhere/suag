package native

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/pkg/agentcore/api"
)

// removeFixture builds a workspace with a .gitignore'd tmp/ and returns it
// with a file_remove tool whose trash is outside it.
func removeFixture(t *testing.T) (string, *FileRemoveTool) {
	t.Helper()
	ws := t.TempDir()
	write := func(rel string, n int) {
		for i := 0; i < n; i++ {
			p := filepath.Join(ws, filepath.FromSlash(rel))
			if n > 1 {
				p = filepath.Join(p, fmt.Sprintf("f%04d.txt", i))
			}
			assert.NoErr(t, os.MkdirAll(filepath.Dir(p), 0o755))
			assert.NoErr(t, os.WriteFile(p, []byte("x"), 0o644))
		}
	}
	write(".gitignore", 1)
	assert.NoErr(t, os.WriteFile(filepath.Join(ws, ".gitignore"), []byte("tmp/\n.env\n"), 0o644))
	write(".env", 1)
	write("a.txt", 1)
	write(".git/HEAD", 1)
	write("src/x.go", 1)
	write("tmp/proj/.git/HEAD", 1)
	write("tmp/proj/main.go", 1)
	write("tmp/proj/node_modules/m", 3)
	return ws, NewFileRemoveTool(ws, filepath.Join(t.TempDir(), "trash"))
}

func runRemove(t *testing.T, tool *FileRemoveTool, args map[string]any) (string, error) {
	t.Helper()
	res, err := tool.Execute(context.Background(), api.ToolCall{CallID: "c", Arguments: args})
	if err != nil {
		return "", err
	}
	return res.Content, nil
}

func TestFileRemove(t *testing.T) {
	t.Run("a file goes to the trash with its origin", func(t *testing.T) {
		ws, tool := removeFixture(t)
		out, err := runRemove(t, tool, map[string]any{"path": "a.txt"})
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, out, "removed file a.txt, 1 bytes; kept in the trash for 7 days")
		_, err = os.Stat(filepath.Join(ws, "a.txt"))
		assert.True(t, os.IsNotExist(err))
		entries, _ := os.ReadDir(tool.trashDir)
		assert.Require(t, assert.Eq(t, 1, len(entries)))
		entry := filepath.Join(tool.trashDir, entries[0].Name())
		_, err = os.Stat(filepath.Join(entry, "a.txt"))
		assert.NoErr(t, err)
		origin, err := os.ReadFile(filepath.Join(entry, "origin.json"))
		assert.NoErr(t, err)
		assert.StrContains(t, string(origin), `"files":1`)
	})
	t.Run("a nested test repository under the ignored tmp/ is deleted for good", func(t *testing.T) {
		ws, tool := removeFixture(t)
		plan, err := PlanRemove(ws, map[string]any{"path": "tmp/proj", "recursive": true})
		assert.Require(t, assert.NoErr(t, err))
		assert.True(t, plan.Purge)
		out, err := runRemove(t, tool, map[string]any{"path": "tmp/proj", "recursive": true})
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, out, "removed tmp/proj permanently")
		_, err = os.Stat(filepath.Join(ws, "tmp", "proj"))
		assert.True(t, os.IsNotExist(err))
		entries, _ := os.ReadDir(tool.trashDir)
		assert.Eq(t, 0, len(entries)) // nothing went to the trash
		// A file inside an ignored directory is regenerable too.
		assert.NoErr(t, os.WriteFile(filepath.Join(ws, "tmp", "log.txt"), nil, 0o644))
		plan, err = PlanRemove(ws, map[string]any{"path": "tmp/log.txt"})
		assert.NoErr(t, err)
		assert.True(t, plan.Purge)
	})
	t.Run("ignored directories inside are deleted for good, the rest goes to the trash", func(t *testing.T) {
		ws, tool := removeFixture(t)
		for i := 0; i <= RemoveMaxFiles; i++ { // more than the limit, never counted
			p := filepath.Join(ws, "app", "node_modules", "pkg", fmt.Sprintf("f%04d.js", i))
			assert.NoErr(t, os.MkdirAll(filepath.Dir(p), 0o755))
			assert.NoErr(t, os.WriteFile(p, nil, 0o644))
		}
		assert.NoErr(t, os.WriteFile(filepath.Join(ws, "app", "main.js"), []byte("main"), 0o644))
		plan, err := PlanRemove(ws, map[string]any{"path": "app", "recursive": true})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, int64(1), plan.Files)
		assert.Eq(t, []string{"app/node_modules"}, plan.PurgeDirs)
		out, err := runRemove(t, tool, map[string]any{"path": "app", "recursive": true})
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, out, "removed directory (1 files) app")
		assert.StrContains(t, out, "1 ignored (regenerable) directory deleted permanently: app/node_modules/")
		matches, _ := filepath.Glob(filepath.Join(tool.trashDir, "*", "app", "main.js"))
		assert.Eq(t, 1, len(matches))
		gone, _ := filepath.Glob(filepath.Join(tool.trashDir, "*", "app", "node_modules"))
		assert.Eq(t, 0, len(gone))
	})
	t.Run("an ignored single file such as .env still goes to the trash", func(t *testing.T) {
		_, tool := removeFixture(t)
		out, err := runRemove(t, tool, map[string]any{"path": ".env"})
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, out, "kept in the trash")
	})
	t.Run("refusals", func(t *testing.T) {
		ws, tool := removeFixture(t)
		for args, want := range map[*map[string]any]string{
			{"path": "."}:                       "refusing to remove the workspace root",
			{"path": ws}:                        "refusing to remove the workspace root",
			{"path": ".git", "recursive": true}: "workspace's own .git",
			{"path": ".git/HEAD"}:               "workspace's own .git",
			{"path": "*.txt"}:                   "wildcards are not expanded",
			{"path": "../outside"}:              "outside workspace",
			{"path": "src"}:                     "is a directory; pass recursive=true",
			{"path": "missing.txt"}:             "missing.txt does not exist",
			{"path": ""}:                        `"path" arg is required`,
		} {
			_, err := runRemove(t, tool, *args)
			assert.ErrMsgContains(t, err, want, (*args)["path"])
		}
		_, err := os.Stat(filepath.Join(ws, ".git", "HEAD"))
		assert.NoErr(t, err)
	})
	t.Run("the home directory is never removed", func(t *testing.T) {
		ws, tool := removeFixture(t)
		home := filepath.Join(ws, "users", "me")
		assert.NoErr(t, os.MkdirAll(home, 0o755))
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		for _, p := range []string{"users/me", "users"} {
			_, err := runRemove(t, tool, map[string]any{"path": p, "recursive": true})
			assert.ErrMsgContains(t, err, "home directory", p)
		}
	})
	t.Run("a symlinked parent leaving the workspace is refused; a link is removed as a link", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlinks need privileges on Windows")
		}
		ws, tool := removeFixture(t)
		outside := t.TempDir()
		assert.NoErr(t, os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("k"), 0o644))
		assert.NoErr(t, os.Symlink(outside, filepath.Join(ws, "ext")))
		_, err := runRemove(t, tool, map[string]any{"path": "ext/keep.txt"})
		assert.ErrMsgContains(t, err, "outside workspace")
		_, err = runRemove(t, tool, map[string]any{"path": "ext"})
		assert.NoErr(t, err)
		_, err = os.Stat(filepath.Join(outside, "keep.txt"))
		assert.NoErr(t, err) // the target survives
	})
	t.Run("at most 1000 files go to the trash", func(t *testing.T) {
		ws, _ := removeFixture(t)
		for i := 0; i <= RemoveMaxFiles; i++ {
			p := filepath.Join(ws, "big", fmt.Sprintf("f%04d.go", i))
			assert.NoErr(t, os.MkdirAll(filepath.Dir(p), 0o755))
			assert.NoErr(t, os.WriteFile(p, nil, 0o644))
		}
		_, err := PlanRemove(ws, map[string]any{"path": "big", "recursive": true})
		assert.ErrMsgContains(t, err, "more than 1000 files that would go to the trash")
	})
}

func TestFileRemoveAcrossVolumes(t *testing.T) {
	prev := renamePath
	renamePath = func(string, string) error { return errors.New("invalid cross-device link") }
	t.Cleanup(func() { renamePath = prev })

	ws, tool := removeFixture(t)
	out, err := runRemove(t, tool, map[string]any{"path": "src", "recursive": true})
	assert.Require(t, assert.NoErr(t, err))
	assert.StrContains(t, out, "kept in the trash")
	_, err = os.Stat(filepath.Join(ws, "src"))
	assert.True(t, os.IsNotExist(err))
	matches, _ := filepath.Glob(filepath.Join(tool.trashDir, "*", "src", "x.go"))
	assert.Eq(t, 1, len(matches)) // copied, then removed
}

func TestTrashPurge(t *testing.T) {
	ws, tool := removeFixture(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	tool.now = func() time.Time { return now }
	old := filepath.Join(tool.trashDir, now.Add(-8*24*time.Hour).Format(trashStampLayout)+"-aaaaaa")
	recent := filepath.Join(tool.trashDir, now.Add(-6*24*time.Hour).Format(trashStampLayout)+"-bbbbbb")
	other := filepath.Join(tool.trashDir, "not-an-entry")
	for _, d := range []string{old, recent, other} {
		assert.NoErr(t, os.MkdirAll(d, 0o755))
	}
	_, err := runRemove(t, tool, map[string]any{"path": "a.txt"})
	assert.NoError(t, err)
	_, err = os.Stat(old)
	assert.True(t, os.IsNotExist(err), "older than 7 days is purged")
	for _, d := range []string{recent, other} {
		_, err = os.Stat(d)
		assert.NoErr(t, err)
	}
	_ = ws
	assert.True(t, strings.HasPrefix(filepath.Base(old), "20261012-"))
}

func TestFileRemoveSnapshotHook(t *testing.T) {
	t.Run("catalog injects the snapshotter", func(t *testing.T) {
		snapshots := &recordingSnapshotter{}
		tools := BuildTools(Env{Workspace: t.TempDir(), TrashDir: filepath.Join(t.TempDir(), "trash"), Snapshots: snapshots}, true)
		var remove *FileRemoveTool
		for _, tool := range tools {
			if candidate, ok := tool.(*FileRemoveTool); ok {
				remove = candidate
			}
		}
		assert.Require(t, assert.NotNil(t, remove))
		assert.Eq(t, snapshots, remove.snapshots)
	})

	t.Run("records the actual trash entry and origin after a file removal", func(t *testing.T) {
		ws, tool := removeFixture(t)
		snapshots := &recordingSnapshotter{}
		tool.snapshots = snapshots
		_, err := tool.Execute(t.Context(), api.ToolCall{CallID: "remove/file", Arguments: map[string]any{"path": "a.txt"}})
		assert.Require(t, assert.NoError(t, err))
		assert.Require(t, assert.Len(t, snapshots.before, 1))
		assert.Eq(t, SnapshotKindTrash, snapshots.before[0].Kind)
		assert.Eq(t, "a.txt", snapshots.before[0].Rel)
		assert.Empty(t, snapshots.before[0].Before)
		assert.Require(t, assert.Len(t, snapshots.after, 1))
		assert.Eq(t, SnapshotStatusComplete, snapshots.after[0].Status)
		assert.Eq(t, SnapshotKindTrash, snapshots.after[0].Kind)
		assert.Eq(t, int64(1), snapshots.after[0].Size)
		assert.Require(t, assert.Len(t, snapshots.after[0].TrashEntries, 1))
		entry := snapshots.after[0].TrashEntries[0]
		_, err = os.Stat(filepath.Join(entry, "origin.json"))
		assert.NoErr(t, err)
		_, err = os.Stat(filepath.Join(ws, "a.txt"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("records purged and mixed deletion separately", func(t *testing.T) {
		ws, tool := removeFixture(t)
		purgedSnapshots := &recordingSnapshotter{}
		tool.snapshots = purgedSnapshots
		_, err := tool.Execute(t.Context(), api.ToolCall{CallID: "purged", Arguments: map[string]any{"path": "tmp/proj", "recursive": true}})
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, SnapshotKindPurged, purgedSnapshots.before[0].Kind)
		assert.Eq(t, []string{"tmp/proj"}, purgedSnapshots.after[0].PurgedDirs)
		assert.Empty(t, purgedSnapshots.after[0].TrashEntries)

		assert.NoErr(t, os.MkdirAll(filepath.Join(ws, "app", "node_modules"), 0o755))
		assert.NoErr(t, os.WriteFile(filepath.Join(ws, "app", "node_modules", "cache"), []byte("x"), 0o644))
		assert.NoErr(t, os.WriteFile(filepath.Join(ws, "app", "main.go"), []byte("main"), 0o644))
		mixedSnapshots := &recordingSnapshotter{}
		tool.snapshots = mixedSnapshots
		_, err = tool.Execute(t.Context(), api.ToolCall{CallID: "mixed", Arguments: map[string]any{"path": "app", "recursive": true}})
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, SnapshotKindTrash, mixedSnapshots.before[0].Kind)
		assert.Eq(t, []string{"app/node_modules"}, mixedSnapshots.after[0].PurgedDirs)
		assert.Require(t, assert.Len(t, mixedSnapshots.after[0].TrashEntries, 1))
	})

	t.Run("marks the snapshot incomplete when trash setup fails", func(t *testing.T) {
		ws, tool := removeFixture(t)
		trashFile := filepath.Join(t.TempDir(), "trash")
		assert.NoErr(t, os.WriteFile(trashFile, []byte("not a directory"), 0o644))
		tool.trashDir = trashFile
		snapshots := &recordingSnapshotter{}
		tool.snapshots = snapshots
		_, err := tool.Execute(t.Context(), api.ToolCall{CallID: "failed", Arguments: map[string]any{"path": "a.txt"}})
		assert.Err(t, err)
		assert.Require(t, assert.Len(t, snapshots.after, 1))
		assert.Eq(t, SnapshotStatusIncomplete, snapshots.after[0].Status)
		_, err = os.Stat(filepath.Join(ws, "a.txt"))
		assert.NoError(t, err)
	})
}
