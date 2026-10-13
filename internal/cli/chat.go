package cli

import (
	"context"
	"io"
	"os"

	"github.com/inhere/suga/internal/chat"
	appconfig "github.com/inhere/suga/internal/config"
	sugahooks "github.com/inhere/suga/internal/hooks"
	"github.com/inhere/suga/internal/wiring"
	"github.com/inhere/suga/pkg/agentcore/tasks"
)

// runChat starts the interactive REPL on an assembled runner.
// The runtime's asker (chat approver questions) is bound by chat when stdin is a terminal.
func runChat(ctx context.Context, runtime *wiring.Runtime, opts personalOptions, stdin io.Reader, stdout, stderr io.Writer,
	taskManager *tasks.Manager, taskWake <-chan struct{}) error {
	hooks, err := sugahooks.New()
	if err != nil {
		return err
	}
	idle, err := appconfig.ReadIdleSettings()
	if err != nil {
		return err
	}
	return chat.Run(ctx, runtime.Runner, chat.Options{
		SessionID: opts.SessionID, ResumeID: opts.ResumeID, FromID: opts.FromID,
		Asker: runtime.Asker, WriteTools: runtime.WriteTools,
		Thinking: runtime.Thinking, ContextWindow: runtime.ContextWindow,
		IdleAfter: idle.After, IdleMinRatio: idle.MinRatio,
		Tasks: taskManager, TaskWake: taskWake, StopBackground: sugahooks.StopBackground(),
	}, stdin, stdout, stderr, hooks)
}

// RunPersonalChat gives the terminal REPL the same persistent session engine as run mode.
func RunPersonalChat(ctx context.Context, args []string) error {
	return RunPersonalCLI(ctx, "chat", args, os.Stdin, os.Stdout, os.Stderr)
}
