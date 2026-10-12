package cli

import (
	"context"
	"io"
	"os"

	"github.com/inhere/suag/internal/chat"
	appconfig "github.com/inhere/suag/internal/config"
	suaghooks "github.com/inhere/suag/internal/hooks"
	"github.com/inhere/suag/internal/wiring"
)

// runChat starts the interactive REPL on an assembled runner.
// The runtime's asker (chat approver questions) is bound by chat when stdin is a terminal.
func runChat(ctx context.Context, runtime *wiring.Runtime, opts personalOptions, stdin io.Reader, stdout, stderr io.Writer) error {
	hooks, err := suaghooks.New()
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
	}, stdin, stdout, stderr, hooks)
}

// RunPersonalChat gives the terminal REPL the same persistent session engine as run mode.
func RunPersonalChat(ctx context.Context, args []string) error {
	return RunPersonalCLI(ctx, "chat", args, os.Stdin, os.Stdout, os.Stderr)
}
