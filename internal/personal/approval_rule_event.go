package personal

import (
	"context"
	"log/slog"

	appconfig "github.com/inhere/suga/internal/config"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore/api"
)

const approvalRulePersistEvent = "approval_rule_persist"

// RecordApprovalRulePersist adapts a chat writeback result to the personal trace
// store. The chat approver is assembled before the Runner, so it uses the public
// session id in the turn context and opens the shared store for this best-effort
// host event instead of depending on wiring internals.
func RecordApprovalRulePersist(ctx context.Context, file, action, result string) {
	sessionID := api.SessionIDFrom(ctx)
	if sessionID == "" {
		return
	}
	dbPath, err := appconfig.ResolveCoreStorePath()
	if err != nil {
		slog.Warn("resolve approval rule persistence trace", "session_id", sessionID, "err", err)
		return
	}
	repo, err := store.Open(dbPath)
	if err != nil {
		slog.Warn("open approval rule persistence trace store", "session_id", sessionID, "err", err)
		return
	}
	defer repo.Close()
	_, err = repo.AppendEvent(context.WithoutCancel(ctx), store.Event{
		SessionID: sessionID,
		TurnID:    observeScopeFrom(ctx).turnID,
		Kind:      approvalRulePersistEvent,
		Status:    result,
		Attrs: map[string]any{
			"file":   file,
			"action": action,
			"result": result,
		},
	})
	if err != nil {
		slog.Warn("record approval rule persistence", "session_id", sessionID, "err", err)
	}
}
