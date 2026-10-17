package personal

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/tools"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

type observeSnapshotter struct {
	before   native.SnapshotBefore
	afterErr error
}

func (s observeSnapshotter) BeforeCall(context.Context, native.SnapshotRequest) (native.SnapshotBefore, error) {
	return s.before, nil
}

func (s observeSnapshotter) AfterCall(context.Context, native.SnapshotResult) error {
	return s.afterErr
}

func TestToolEventsCarrySnapshotAttrs(t *testing.T) {
	tests := []struct {
		name       string
		snapshot   native.SnapshotBefore
		afterErr   error
		wantStatus string
		wantReason string
	}{
		{name: "complete", wantStatus: native.SnapshotStatusComplete},
		{name: "oversize", snapshot: native.SnapshotBefore{Unrecoverable: native.SnapshotUnrecoverableOversize}, wantStatus: native.SnapshotStatusComplete, wantReason: native.SnapshotUnrecoverableOversize},
		{name: "incomplete", afterErr: errors.New("snapshot finalization failed"), wantStatus: native.SnapshotStatusIncomplete},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := openObserveRepo(t)
			workspace := t.TempDir()
			snapshots := observeSnapshotter{before: tc.snapshot, afterErr: tc.afterErr}
			var write agentcore.Tool
			for _, candidate := range native.BuildTools(native.Env{Workspace: workspace, Snapshots: snapshots}, true) {
				if candidate.Name() == "file_write" {
					write = candidate
				}
			}
			assert.Require(t, assert.NotNil(t, write))
			registry := tools.NewRegistry()
			registry.Register(write)
			recorder := &recorder{sink: repo, workspace: workspace}
			observed := recorder.observeTools(registry)
			sessionID := "snapshot-session-" + tc.name
			ctx := withObserveScope(context.Background(), sessionID, "snapshot-turn")
			_, _ = observed.ExecuteTool(ctx, agentcore.ToolCall{CallID: "snapshot-call", Name: "file_write",
				Arguments: map[string]any{"path": "safe.txt", "content": "secret before bytes"}})

			events := eventsOf(t, repo, sessionID, store.EventTool)
			assert.Require(t, assert.Len(t, events, 1))
			snapshot, ok := events[0].Attrs["snapshot"].(map[string]any)
			assert.Require(t, assert.True(t, ok))
			assert.Eq(t, tc.wantStatus, snapshot["status"])
			assert.Eq(t, float64(0), snapshot["bytes"])
			assert.Eq(t, tc.wantReason, snapshot["unrecoverable"])
			assert.NotContains(t, events[0].Attrs, "before")
			line := traceLine(events[0])
			assert.StrContains(t, line, "snapshot=absent "+tc.wantStatus)
			if tc.wantReason != "" {
				assert.StrContains(t, line, "unrecoverable="+tc.wantReason)
			}
			var jsonTrace bytes.Buffer
			assert.NoError(t, WriteTrace(&jsonTrace, events, nil, true))
			assert.StrContains(t, jsonTrace.String(), `"snapshot"`)
			assert.NotContains(t, jsonTrace.String(), "secret before bytes")
		})
	}
}
