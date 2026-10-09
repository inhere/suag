package cli

import (
	"context"
	"io"
	"os"

	"github.com/inhere/suag/internal/chat"
	suaghooks "github.com/inhere/suag/internal/hooks"
	"github.com/inhere/suag/internal/personal"
)

// runChat starts the interactive REPL on an assembled runner.
func runChat(ctx context.Context, runner *personal.Runner, opts personalOptions, stdin io.Reader, stdout, stderr io.Writer) error {
	hooks, err := suaghooks.New()
	if err != nil {
		return err
	}
	return chat.Run(ctx, runner, chat.Options{SessionID: opts.SessionID, ResumeID: opts.ResumeID, FromID: opts.FromID}, stdin, stdout, stderr, hooks)
}

// RunPersonalChat gives the terminal REPL the same persistent session engine as run mode.
func RunPersonalChat(ctx context.Context, args []string) error {
	return RunPersonalCLI(ctx, "chat", args, os.Stdin, os.Stdout, os.Stderr)
}
