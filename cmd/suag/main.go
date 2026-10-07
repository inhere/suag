// Command suag runs the personal agent through CLI or ACP stdio.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/gookit/goutil/cflag/capp"
	"github.com/inhere/suag/internal/app"
	appconfig "github.com/inhere/suag/internal/config"
)

var Version = "dev"

func main() {
	if err := appconfig.Load(); err != nil {
		fmt.Fprintln(os.Stderr, "load config:", err)
		os.Exit(1)
	}
	app.SetupLogging()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"chat"}
	}
	if err := newCLI(ctx, os.Stdin, os.Stdout, os.Stderr).RunWithArgs(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newCLI(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) *capp.App {
	cli := capp.NewApp()
	cli.Name = "suag"
	cli.Desc = "Personal agent with CLI and ACP interfaces"
	cli.Version = Version
	cli.SetOutput(stdout)
	acp := capp.NewCmd("acp", "Serve an ACP session over stdio", func(c *capp.Cmd) error {
		if len(c.RemainArgs()) != 0 {
			return fmt.Errorf("suag acp does not accept arguments")
		}
		if err := app.RunPersonalACP(ctx, stdin, stdout); err != nil {
			return fmt.Errorf("acp: %w", err)
		}
		return nil
	})
	acp.SetOutput(stderr)
	cli.Add(
		app.NewPersonalCommand(ctx, "run", stdin, stdout, stderr),
		app.NewPersonalCommand(ctx, "chat", stdin, stdout, stderr),
		acp,
	)
	return cli
}
