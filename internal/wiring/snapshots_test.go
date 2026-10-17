package wiring

import (
	"context"
	"errors"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore/api"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

func TestNativeEnvSnapshotConfiguration(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	t.Setenv("SUGA_HOME", t.TempDir())
	enabled := nativeEnvWithSnapshot(t.TempDir(), nil, configuredSnapshotter())
	assert.NotNil(t, enabled.Snapshots)

	assert.NoErr(t, gkconfig.Set("tools.snapshots.enabled", false))
	disabled := nativeEnvWithSnapshot(t.TempDir(), nil, configuredSnapshotter())
	assert.Nil(t, disabled.Snapshots)
}

func TestSnapshotSettingsUseConfiguredLimits(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	t.Setenv("SUGA_HOME", t.TempDir())
	assert.NoErr(t, gkconfig.Set("tools.snapshots", map[string]any{
		"enabled":           true,
		"max_file_bytes":    123,
		"max_session_bytes": 456,
		"max_global_bytes":  789,
		"keep_turns":        2,
		"keep_days":         3,
	}))

	policy, ok := nativeEnvWithSnapshot(t.TempDir(), nil, configuredSnapshotter()).Snapshots.(*policySnapshotter)
	assert.True(t, ok)
	assert.Eq(t, int64(123), policy.store.Limits().MaxFileBytes)
	assert.Eq(t, int64(456), policy.store.Limits().MaxSessionBytes)
	assert.Eq(t, int64(789), policy.store.Limits().MaxGlobalBytes)
	assert.Eq(t, 2, policy.store.Limits().KeepTurns)
	assert.Eq(t, 3, policy.store.Limits().KeepDays)
}

func TestSnapshotContinueMakesUnavailableWriteExplicit(t *testing.T) {
	policy := &policySnapshotter{onError: "continue", unavailable: errors.New("directory is not writable")}
	ctx := api.WithSessionID(context.Background(), "session")
	before, err := policy.BeforeCall(ctx, native.SnapshotRequest{CallID: "call"})
	assert.NoErr(t, err)
	assert.Eq(t, snapshotUnrecoverableError, before.Unrecoverable)
	assert.StrContains(t, before.Notice, "不可撤销")

	policy.onError = "deny"
	_, err = policy.BeforeCall(ctx, native.SnapshotRequest{CallID: "call"})
	assert.ErrMsgContains(t, err, "not writable")
}
