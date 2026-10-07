// Command suag runs the personal agent through CLI or ACP stdio.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/gookit/goutil/cflag"
	"github.com/gookit/goutil/cflag/capp"
	"github.com/inhere/suag/internal/app"
	appconfig "github.com/inhere/suag/internal/config"
)

var (
	Version   = "dev"
	GitCommit string
	BuildDate string
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"chat"}
	}
	// Version must remain available even when a user's model config is invalid.
	if len(args) != 1 || (args[0] != "version" && args[0] != "--version" && args[0] != "-V") {
		if err := appconfig.Load(); err != nil {
			fmt.Fprintln(os.Stderr, "load config:", err)
			os.Exit(1)
		}
	}
	app.SetupLogging()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := newCLI(ctx, os.Stdin, os.Stdout, os.Stderr).RunWithArgs(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func formatVersion(version, commit string) string {
	if version == "" {
		version = "dev"
	}
	if commit == "" {
		commit = "dev"
	}
	return "suag " + version + " (" + commit + ")"
}

func newCLI(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) *capp.App {
	var showVersion bool
	cli := capp.NewApp()
	cli.Name = "suag"
	cli.Desc = "Personal agent with CLI and ACP interfaces"
	cli.Version = Version
	cli.SetOutput(stdout)
	cli.BoolVar(&showVersion, "version", false, "show version then exit;false;V")
	cli.LongHelp = `Examples:
  suag                         Start interactive chat
  suag run --prompt "hello"    Run one turn
  suag acp                     Serve ACP over stdio
`

	cli.AfterFlagParse = func(c *cflag.CFlags) bool {
		if showVersion {
			fmt.Fprintln(c.Output(), formatVersion(Version, GitCommit))
			return false
		}
		return true
	}

	// commands
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
	version := capp.NewCmd("version", "Print version and commit", func(c *capp.Cmd) error {
		if len(c.RemainArgs()) != 0 {
			return fmt.Errorf("suag version does not accept arguments")
		}
		_, err := fmt.Fprintln(stdout, formatVersion(Version, GitCommit))
		return err
	})
	version.SetOutput(stderr)
	cli.Add(
		app.NewPersonalCommand(ctx, "run", stdin, stdout, stderr),
		app.NewPersonalCommand(ctx, "chat", stdin, stdout, stderr),
		acp,
		version,
	)
	return cli
}
