package native

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/pkg/agentcore/api"
)

func TestFileRemoveSnapshotReportMarksPurgedContentUnrecoverable(t *testing.T) {
	_, tool := removeFixture(t)
	snapshots := &snapshotReportHook{}
	tool.snapshots = snapshots
	ctx, report := WithSnapshotReport(context.Background())
	_, err := tool.Execute(ctx, api.ToolCall{CallID: "purged-report", Arguments: map[string]any{
		"path": "tmp/proj", "recursive": true,
	}})
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, SnapshotKindPurged, report.Kind)
	assert.Eq(t, SnapshotStatusComplete, report.Status)
	assert.Eq(t, SnapshotUnrecoverablePurged, report.Unrecoverable)
}
