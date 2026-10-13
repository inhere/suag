package wiring

import (
	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/pkg/agentcore/tasks"
)

// NewTaskManager returns the background task manager of a host process
// (chat, run) with the tasks.* limits; task output goes to the session's
// outputs directory, so recall reads it. onExit (may be nil) is told of
// tasks that ended without the model seeing it. The host closes it.
func NewTaskManager(onExit func(tasks.Info)) (*tasks.Manager, appconfig.TaskSettings, error) {
	settings, err := appconfig.ReadTaskSettings()
	if err != nil {
		return nil, settings, err
	}
	dir, err := appconfig.ResolveSessionsDir()
	if err != nil {
		return nil, settings, err
	}
	spiller := personal.NewSessionSpiller(dir)
	m := tasks.NewManager(tasks.Config{Limits: settings.Limits, Output: spiller.TaskOutput, OnExit: onExit})
	return m, settings, nil
}
