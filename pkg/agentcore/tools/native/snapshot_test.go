package native

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/pkg/agentcore/api"
)

type recordingSnapshotter struct {
	beforeResult SnapshotBefore
	beforeErr    error
	afterErr     error
	before       []SnapshotRequest
	after        []SnapshotResult
}

func (s *recordingSnapshotter) BeforeCall(_ context.Context, req SnapshotRequest) (SnapshotBefore, error) {
	s.before = append(s.before, req)
	return s.beforeResult, s.beforeErr
}

func (s *recordingSnapshotter) AfterCall(_ context.Context, result SnapshotResult) error {
	s.after = append(s.after, result)
	return s.afterErr
}

func writeCall(path, content, id string) api.ToolCall {
	return api.ToolCall{
		CallID: id,
		Name:   "file_write",
		Arguments: map[string]any{
			"path":    path,
			"content": content,
		},
	}
}

func TestFileWriteSnapshotHook(t *testing.T) {
	t.Run("catalog injects the snapshotter", func(t *testing.T) {
		snapshots := &recordingSnapshotter{}
		tools := BuildTools(Env{Workspace: t.TempDir(), Snapshots: snapshots}, true)
		var write *FileWriteTool
		for _, tool := range tools {
			if candidate, ok := tool.(*FileWriteTool); ok {
				write = candidate
			}
		}
		assert.Require(t, assert.NotNil(t, write))
		assert.Eq(t, snapshots, write.snapshots)
	})

	t.Run("records binary before and after for repeated calls", func(t *testing.T) {
		workspace := t.TempDir()
		path := filepath.Join(workspace, "binary.dat")
		original := []byte{0x00, 0xff, 0x0a, 0x80}
		assert.NoErr(t, os.WriteFile(path, original, 0o644))
		snapshots := &recordingSnapshotter{}
		tool := &FileWriteTool{workspace: workspace, snapshots: snapshots}

		_, err := tool.Execute(context.Background(), writeCall("binary.dat", string([]byte{0x01, 0xfe}), "call/one"))
		assert.Require(t, assert.NoErr(t, err))
		_, err = tool.Execute(context.Background(), writeCall("binary.dat", "second", "call/two"))
		assert.Require(t, assert.NoErr(t, err))

		assert.Eq(t, 2, len(snapshots.before))
		assert.Eq(t, 2, len(snapshots.after))
		assert.Eq(t, SnapshotKindFile, snapshots.before[0].Kind)
		assert.Eq(t, original, snapshots.before[0].Before)
		assert.Eq(t, "call/one", snapshots.before[0].CallID)
		assert.Eq(t, "binary.dat", snapshots.before[0].Rel)
		assert.Eq(t, sha256Hex([]byte{0x01, 0xfe}), snapshots.after[0].AfterSHA256)
		assert.Eq(t, SnapshotStatusComplete, snapshots.after[0].Status)
		assert.Eq(t, []byte{0x01, 0xfe}, snapshots.before[1].Before)
		assert.Eq(t, "second", string(mustReadFile(t, path)))
	})

	t.Run("records absent without reading or inventing bytes", func(t *testing.T) {
		snapshots := &recordingSnapshotter{}
		tool := &FileWriteTool{workspace: t.TempDir(), snapshots: snapshots}
		_, err := tool.Execute(context.Background(), writeCall("new.bin", "new", "absent"))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, SnapshotKindAbsent, snapshots.before[0].Kind)
		assert.Empty(t, snapshots.before[0].Before)
		assert.Eq(t, SnapshotStatusComplete, snapshots.after[0].Status)
		assert.Empty(t, snapshots.after[0].AfterSHA256)
	})

	t.Run("fails closed before changing the target", func(t *testing.T) {
		workspace := t.TempDir()
		path := filepath.Join(workspace, "keep.txt")
		assert.NoErr(t, os.WriteFile(path, []byte("keep"), 0o644))
		snapshots := &recordingSnapshotter{beforeErr: errors.New("snapshot store unavailable")}
		tool := &FileWriteTool{workspace: workspace, snapshots: snapshots}

		_, err := tool.Execute(context.Background(), writeCall("keep.txt", "changed", "denied"))
		assert.ErrMsgContains(t, err, "snapshot before")
		assert.Eq(t, "keep", string(mustReadFile(t, path)))
		assert.Empty(t, snapshots.after)
	})

	t.Run("continues oversize writes with an explicit warning", func(t *testing.T) {
		snapshots := &recordingSnapshotter{beforeResult: SnapshotBefore{Unrecoverable: SnapshotUnrecoverableOversize}}
		tool := &FileWriteTool{workspace: t.TempDir(), snapshots: snapshots}
		result, err := tool.Execute(context.Background(), writeCall("large.bin", "data", "oversize"))
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, result.Content, "不可撤销")
		assert.Eq(t, SnapshotUnrecoverableOversize, snapshots.after[0].Unrecoverable)
	})

	t.Run("records incomplete when the write fails", func(t *testing.T) {
		workspace := t.TempDir()
		// The write must fail after the snapshot was taken. On Windows a file in
		// place of the parent directory does that; on Unix the path lookup
		// already reports ENOTDIR there, so a read-only directory makes the
		// write itself fail instead.
		target := filepath.Join("parent", "child.txt")
		if runtime.GOOS == "windows" {
			assert.NoErr(t, os.WriteFile(filepath.Join(workspace, "parent"), []byte("file"), 0o644))
		} else {
			if os.Geteuid() == 0 {
				t.Skip("root ignores directory permissions")
			}
			dir := filepath.Join(workspace, "parent")
			assert.NoErr(t, os.Mkdir(dir, 0o755))
			assert.NoErr(t, os.Chmod(dir, 0o555))
			t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		}
		snapshots := &recordingSnapshotter{}
		tool := &FileWriteTool{workspace: workspace, snapshots: snapshots}
		_, err := tool.Execute(context.Background(), writeCall(target, "nope", "write-error"))
		assert.Err(t, err)
		assert.Require(t, assert.Len(t, snapshots.after, 1))
		assert.Eq(t, SnapshotStatusIncomplete, snapshots.after[0].Status)
		assert.Empty(t, snapshots.after[0].AfterSHA256)
	})

	t.Run("reports after failure after the file has been written", func(t *testing.T) {
		workspace := t.TempDir()
		snapshots := &recordingSnapshotter{afterErr: errors.New("cannot finalize")}
		tool := &FileWriteTool{workspace: workspace, snapshots: snapshots}
		_, err := tool.Execute(context.Background(), writeCall("written.txt", "written", "after-error"))
		assert.ErrMsgContains(t, err, "snapshot after failed after writing")
		assert.Eq(t, "written", string(mustReadFile(t, filepath.Join(workspace, "written.txt"))))
	})
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	assert.Require(t, assert.NoErr(t, err))
	return data
}
