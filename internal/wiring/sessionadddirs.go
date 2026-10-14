package wiring

import (
	"context"
	"log/slog"

	"github.com/inhere/suga/internal/adddir"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

// ApplyAddDirs publishes the session's additional directories (--add-dir): the
// ones its metadata already carries plus the ones this invocation requested (a
// path named again takes this invocation's mode). The metadata records the
// requested mode, so resuming the session keeps the directories and their
// permissions; the tools see the effective view, which a read-only runtime
// downgrades (it has no write tool at all).
//
// The front end calls it once the session is open (run: after openRequestedSession,
// chat: through chat.Options.SetupSession) and before the first turn.
func (rt *Runtime) ApplyAddDirs(ctx context.Context, sessionID string) error {
	if rt == nil || rt.AddDirs == nil || sessionID == "" {
		return nil
	}
	model, err := rt.Runner.LoadSession(ctx, sessionID)
	if err != nil {
		return err
	}
	stored, err := adddir.FromMeta(model.Meta[adddir.MetaKey])
	if err != nil {
		// A hand-edited or older value must not stop the session.
		slog.Warn("ignore stored session add_dirs", "session_id", sessionID, "err", err)
		stored = nil
	}
	merged := adddir.Merge(stored, rt.requested)
	if !adddir.Equal(stored, merged) {
		if err := rt.Runner.SaveSessionMeta(ctx, sessionID, map[string]any{adddir.MetaKey: adddir.ToMeta(merged)}); err != nil {
			return err
		}
	}
	rt.AddDirs.Set(nativeAddDirs(adddir.Effective(merged, rt.readOnly)))
	return nil
}

// nativeAddDirs converts the session's directories into the native tool set.
func nativeAddDirs(dirs []adddir.Dir) []native.AddDir {
	out := make([]native.AddDir, len(dirs))
	for i, dir := range dirs {
		out[i] = native.AddDir{Path: dir.Path, Writable: dir.Writable}
	}
	return out
}
