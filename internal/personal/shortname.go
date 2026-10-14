package personal

import (
	"context"
	"strings"
)

// metaShortName is the session meta key holding the short name a chat process
// registers under ("<workspace directory>-k7", suga-wy40). It is written the
// first time a session registers its local session socket and kept across
// resumes, so `suga send --session <short name>` keeps addressing it. Sessions
// that never registered (run, ACP) have no short name.
const metaShortName = "short_name"

// SessionShortName returns the short name stored in the session meta, or "" when
// the session has none.
func (r *Runner) SessionShortName(ctx context.Context, sessionID string) (string, error) {
	model, err := r.LoadSession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	name, _ := model.Meta[metaShortName].(string)
	return strings.TrimSpace(name), nil
}

// SaveSessionShortName stores the short name of a session that has none yet: a
// resumed session keeps the name it was first given, even when two chats open it
// at the same time. An empty name is ignored.
func (r *Runner) SaveSessionShortName(ctx context.Context, sessionID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	// Patch-if-absent: the meta write stays atomic with the "has none yet" check.
	_, err := r.cfg.Repository.PatchSessionMetaIfAbsent(ctx, sessionID, metaShortName, map[string]any{metaShortName: name})
	return err
}
