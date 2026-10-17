package native

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

type snapshotReportHook struct {
	beforeResult SnapshotBefore
	beforeHook   func()
	afterErr     error
}

func (s *snapshotReportHook) BeforeCall(context.Context, SnapshotRequest) (SnapshotBefore, error) {
	if s.beforeHook != nil {
		s.beforeHook()
	}
	return s.beforeResult, nil
}

func (s *snapshotReportHook) AfterCall(context.Context, SnapshotResult) error { return s.afterErr }

func TestSnapshotReportFollowsNativeWriteOutcome(t *testing.T) {
	t.Run("complete report contains only the safe summary", func(t *testing.T) {
		workspace := t.TempDir()
		snapshots := &snapshotReportHook{}
		tool := &FileWriteTool{workspace: workspace, snapshots: snapshots}
		ctx, report := WithSnapshotReport(context.Background())
		_, err := tool.Execute(ctx, writeCall("created.txt", "secret before-image must not escape", "report-complete"))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, SnapshotKindAbsent, report.Kind)
		assert.Eq(t, SnapshotStatusComplete, report.Status)
		assert.Eq(t, int64(0), report.Bytes)
		assert.Empty(t, report.Unrecoverable)
	})

	t.Run("oversize keeps the reason while the write succeeds", func(t *testing.T) {
		workspace := t.TempDir()
		snapshots := &snapshotReportHook{beforeResult: SnapshotBefore{Unrecoverable: SnapshotUnrecoverableOversize}}
		tool := &FileWriteTool{workspace: workspace, snapshots: snapshots}
		ctx, report := WithSnapshotReport(context.Background())
		_, err := tool.Execute(ctx, writeCall("large.txt", "content", "report-oversize"))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, SnapshotKindAbsent, report.Kind)
		assert.Eq(t, SnapshotStatusComplete, report.Status)
		assert.Eq(t, SnapshotUnrecoverableOversize, report.Unrecoverable)
	})

	t.Run("failed snapshot finalization is incomplete", func(t *testing.T) {
		workspace := t.TempDir()
		snapshots := &snapshotReportHook{afterErr: errors.New("snapshot store unavailable")}
		tool := &FileWriteTool{workspace: workspace, snapshots: snapshots}
		ctx, report := WithSnapshotReport(context.Background())
		_, err := tool.Execute(ctx, writeCall("written.txt", "written", "report-incomplete"))
		assert.ErrMsgContains(t, err, "snapshot after failed")
		assert.Eq(t, SnapshotStatusIncomplete, report.Status)
		assert.Eq(t, int64(0), report.Bytes)
		assert.Eq(t, "written", string(mustReadFile(t, filepath.Join(workspace, "written.txt"))))
	})

	t.Run("write failure reports incomplete before returning", func(t *testing.T) {
		workspace := t.TempDir()
		parent := filepath.Join(workspace, "parent")
		assert.NoError(t, os.Mkdir(parent, 0o755))
		snapshots := &snapshotReportHook{beforeHook: func() {
			assert.NoError(t, os.Remove(parent))
			assert.NoError(t, os.WriteFile(parent, []byte("blocking file"), 0o644))
		}}
		tool := &FileWriteTool{workspace: workspace, snapshots: snapshots}
		ctx, report := WithSnapshotReport(context.Background())
		_, err := tool.Execute(ctx, writeCall("parent/child.txt", "nope", "report-write-failure"))
		assert.Err(t, err)
		assert.Eq(t, SnapshotStatusIncomplete, report.Status)
	})
}
