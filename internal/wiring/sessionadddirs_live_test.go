package wiring

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/internal/adddir"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore/api"
)

// addDirEvents reads the session's authorization-change events (store.EventAddDir).
func addDirEvents(t *testing.T, rt *Runtime, sessionID string) []store.Event {
	t.Helper()
	events, err := rt.repo.ListEvents(context.Background(), store.EventFilter{SessionID: sessionID})
	assert.Require(t, assert.NoErr(t, err))
	var out []store.Event
	for _, e := range events {
		if e.Kind == store.EventAddDir {
			out = append(out, e)
		}
	}
	return out
}

// TestSessionAddDirsLive covers the handle chat's commands and the
// outside-directory question work through: authorizing and revoking a directory
// changes the session metadata and the set the tools read at once, and every
// change is recorded in the trace.
func TestSessionAddDirsLive(t *testing.T) {
	ctx := context.Background()
	ws, seed := t.TempDir(), t.TempDir()
	addDirsSessionConfig(t, ws)
	rt, sessionID := openAddDirsChatSession(t, Options{AllowWrite: true, AddDirs: []adddir.Dir{{Path: seed, Source: adddir.SourceFlag}}})
	handle := rt.SessionAddDirs()
	assert.Require(t, assert.NotNil(t, handle))
	granted := t.TempDir()

	t.Run("the workspace and the added directories are listed", func(t *testing.T) {
		got, err := handle.List(ctx)
		assert.NoErr(t, err)
		assert.Eq(t, []adddir.Dir{
			{Path: ws, Writable: true},
			{Path: seed, Source: adddir.SourceFlag},
		}, got)
	})
	t.Run("a directory typed in chat takes effect at once", func(t *testing.T) {
		got, err := handle.Add(ctx, granted+":rw", adddir.SourceSession)
		assert.NoErr(t, err)
		assert.Eq(t, adddir.Dir{Path: granted, Writable: true, Source: adddir.SourceSession}, got)

		// The tools read the live set: the write tool accepts the new directory.
		write, err := rt.Registry.Get("file_write")
		assert.Require(t, assert.NoErr(t, err))
		_, err = write.Execute(ctx, api.ToolCall{CallID: "c", Arguments: map[string]any{
			"path": filepath.Join(granted, "a.txt"), "content": "x\n"}})
		assert.NoErr(t, err)
		// And the session keeps it.
		assert.Eq(t, []adddir.Dir{{Path: seed, Source: adddir.SourceFlag}, {Path: granted, Writable: true, Source: adddir.SourceSession}},
			storedAddDirs(t, rt, sessionID))
	})
	t.Run("the change is in the trace", func(t *testing.T) {
		events := addDirEvents(t, rt, sessionID)
		assert.Require(t, assert.Len(t, events, 1))
		assert.Eq(t, "add", events[0].Name)
		assert.Eq(t, granted, events[0].Attrs["path"])
		assert.Eq(t, "rw", events[0].Attrs["mode"])
		assert.Eq(t, "keyboard", events[0].Attrs["from"])
	})
	t.Run("a path given again takes the new mode and source", func(t *testing.T) {
		got, err := handle.Add(ctx, granted+":ro", adddir.SourceAsk)
		assert.NoErr(t, err)
		assert.Eq(t, adddir.Dir{Path: granted, Source: adddir.SourceAsk}, got)
		assert.Eq(t, []string{seed + ":ro", granted + ":ro"}, rt.AddDirs.List())
		events := addDirEvents(t, rt, sessionID)
		assert.Eq(t, "ro", events[len(events)-1].Attrs["mode"])
		assert.Eq(t, "ask", events[len(events)-1].Attrs["from"])
	})
	t.Run("a relative or missing directory is refused", func(t *testing.T) {
		_, err := handle.Add(ctx, "docs", adddir.SourceSession)
		assert.ErrMsgContains(t, err, "is not an absolute path")
		_, err = handle.Add(ctx, filepath.Join(granted, "missing"), adddir.SourceSession)
		assert.ErrMsgContains(t, err, "is not an existing directory")
		_, err = handle.Add(ctx, ":ro", adddir.SourceSession)
		assert.ErrMsgContains(t, err, "names no directory")
	})
	t.Run("revoking writes back to the metadata and the live set", func(t *testing.T) {
		// The mode suffix is accepted (a value copied from :dirs carries one).
		removed, err := handle.Remove(ctx, granted+":ro")
		assert.NoErr(t, err)
		assert.True(t, removed)
		assert.Eq(t, []string{seed + ":ro"}, rt.AddDirs.List())
		assert.Eq(t, []adddir.Dir{{Path: seed, Source: adddir.SourceFlag}}, storedAddDirs(t, rt, sessionID))

		removed, err = handle.Remove(ctx, granted)
		assert.NoErr(t, err)
		assert.False(t, removed) // already gone
		_, err = handle.Remove(ctx, "docs")
		assert.ErrMsgContains(t, err, "需要一个绝对路径")
		events := addDirEvents(t, rt, sessionID)
		assert.Eq(t, "remove", events[len(events)-1].Name)
		assert.Eq(t, granted, events[len(events)-1].Attrs["path"])
	})
	t.Run("a read-only session only accepts read-only directories", func(t *testing.T) {
		readOnlyRT, _ := openAddDirsChatSession(t, Options{ReadOnly: true})
		_, err := readOnlyRT.SessionAddDirs().Add(ctx, granted+":rw", adddir.SourceAsk)
		assert.ErrMsgContains(t, err, "只能授权只读目录")
		got, err := readOnlyRT.SessionAddDirs().Add(ctx, granted, adddir.SourceAsk)
		assert.NoErr(t, err)
		assert.Eq(t, adddir.Dir{Path: granted, Source: adddir.SourceAsk}, got)
		// The session's list is the effective view: readable, never writable.
		list, err := readOnlyRT.SessionAddDirs().List(ctx)
		assert.NoErr(t, err)
		assert.Eq(t, []adddir.Dir{{Path: ws}, {Path: granted, Source: adddir.SourceAsk}}, list)
	})
	t.Run("a handle without an open session refuses", func(t *testing.T) {
		fresh := openAddDirsRuntime(t, Options{AllowWrite: true})
		_, err := fresh.SessionAddDirs().List(ctx)
		assert.ErrMsgContains(t, err, "会话尚未打开")
		_, err = fresh.SessionAddDirs().Add(ctx, ws, adddir.SourceSession)
		assert.ErrMsgContains(t, err, "会话尚未打开")
		_, err = fresh.SessionAddDirs().Remove(ctx, ws)
		assert.ErrMsgContains(t, err, "会话尚未打开")
	})
}

// TestSessionAddDirsApplyKeepsStoredSource: a directory of an earlier session
// keeps the origin it was recorded with, while one named again by this
// invocation takes its source (the flag).
func TestSessionAddDirsApplyKeepsStoredSource(t *testing.T) {
	ctx := context.Background()
	ws, stored, again := t.TempDir(), t.TempDir(), t.TempDir()
	addDirsSessionConfig(t, ws)
	_, sessionID := openAddDirsChatSession(t, Options{AllowWrite: true, AddDirs: []adddir.Dir{{Path: stored, Source: adddir.SourceFlag}}})

	// A later invocation names one of the two again, this time as an rw flag.
	other := openAddDirsRuntime(t, Options{AllowWrite: true, AddDirs: []adddir.Dir{{Path: again, Writable: true, Source: adddir.SourceFlag}}})
	assert.NoErr(t, other.ApplyAddDirs(ctx, sessionID))
	list, err := other.SessionAddDirs().List(ctx)
	assert.NoErr(t, err)
	assert.Eq(t, []adddir.Dir{
		{Path: ws, Writable: true},
		{Path: stored, Source: adddir.SourceFlag},
		{Path: again, Writable: true, Source: adddir.SourceFlag},
	}, list)
}

// TestSessionAddDirsUnreadableStore: a stored value that cannot be read is
// reported and ignored, and the session keeps working.
func TestSessionAddDirsUnreadableStore(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()
	addDirsSessionConfig(t, ws)
	rt, sessionID := openAddDirsChatSession(t, Options{AllowWrite: true})
	assert.NoErr(t, rt.Runner.SaveSessionMeta(ctx, sessionID, map[string]any{adddir.MetaKey: "junk"}))
	assert.NoErr(t, rt.ApplyAddDirs(ctx, sessionID))
	list, err := rt.SessionAddDirs().List(ctx)
	assert.NoErr(t, err)
	assert.Eq(t, []adddir.Dir{{Path: ws, Writable: true}}, list)
}

// TestSessionAddDirsRemoveMissing: revoking a directory that was already
// removed writes no event.
func TestSessionAddDirsRemoveMissing(t *testing.T) {
	ctx := context.Background()
	ws, dir := t.TempDir(), t.TempDir()
	addDirsSessionConfig(t, ws)
	rt, sessionID := openAddDirsChatSession(t, Options{AllowWrite: true, AddDirs: []adddir.Dir{{Path: dir}}})
	removed, err := rt.SessionAddDirs().Remove(ctx, dir)
	assert.NoErr(t, err)
	assert.True(t, removed)
	removed, err = rt.SessionAddDirs().Remove(ctx, dir)
	assert.NoErr(t, err)
	assert.False(t, removed)
	assert.Len(t, addDirEvents(t, rt, sessionID), 1)
}
