package wiring

import (
	"log/slog"

	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore/tasks"
)

// NewTaskManager returns the background task manager of a host process
// (chat, run, acp) with the tasks.* limits; task output goes to the
// session's outputs directory, so recall reads it. onNotice (may be nil) is
// told of every notice left for the model (a task that ended unseen, a
// running task's notice), e.g. to wake an idle chat. The host closes it.
//
// Every task end is recorded in the session's trace through its own store
// handle, not the turn's runner: ACP opens a runtime per prompt, so a task
// may end between turns (or at shutdown) when no runner is open.
func NewTaskManager(onNotice func(tasks.Info)) (*tasks.Manager, appconfig.TaskSettings, error) {
	settings, err := appconfig.ReadTaskSettings()
	if err != nil {
		return nil, settings, err
	}
	dir, err := appconfig.ResolveSessionsDir()
	if err != nil {
		return nil, settings, err
	}
	dbPath, err := appconfig.ResolveCoreStorePath()
	if err != nil {
		return nil, settings, err
	}
	spiller := personal.NewSessionSpiller(dir)
	m := tasks.NewManager(tasks.Config{Limits: settings.Limits, Output: spiller.TaskOutput, OnNotice: onNotice})
	m.SetOnEnd(func(info tasks.Info) { recordTaskEnd(dbPath, info) })
	return m, settings, nil
}

// recordTaskEnd writes a task end into the store at dbPath. Task ends are
// rare, so it opens the store for each one (WAL and busy_timeout let it
// share the database with the open runtime).
func recordTaskEnd(dbPath string, info tasks.Info) {
	repo, err := store.Open(dbPath)
	if err != nil {
		slog.Warn("record task end", "task", info.ID, "session_id", info.SessionID, "err", err)
		return
	}
	defer func() { _ = repo.Close() }()
	personal.RecordTaskEndIn(repo, info)
}
