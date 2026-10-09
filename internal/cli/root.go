// Package cli defines the suag command tree; cmd/suag only assembles and runs it.
package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/gookit/goutil/cflag"
	"github.com/gookit/goutil/cflag/capp"
)

// Build carries the binary's version metadata injected by cmd/suag.
type Build struct {
	// Version is the semantic version shown by the root app.
	Version string
	// VersionLine is the full "suag <version> (<commit>)" line printed by
	// `suag version` and --version.
	VersionLine string
}

// New builds the root command with all subcommands registered.
func New(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, build Build) *capp.App {
	var showVersion bool
	root := capp.NewApp()
	root.Name = "suag"
	root.Desc = "Personal agent with CLI and ACP interfaces"
	root.Version = build.Version
	root.SetOutput(stdout)
	root.BoolVar(&showVersion, "version", false, "show version then exit;false;V")
	root.LongHelp = `Examples:
  suag                         Start interactive chat
  suag run --prompt "hello"    Run one turn
  suag acp                     Serve ACP over stdio
  suag trace <session> --json  Show the event timeline of a session
`

	root.AfterFlagParse = func(c *cflag.CFlags) bool {
		if showVersion {
			fmt.Fprintln(c.Output(), build.VersionLine)
			return false
		}
		return true
	}

	root.Add(
		NewPersonalCommand(ctx, "run", stdin, stdout, stderr),
		NewPersonalCommand(ctx, "chat", stdin, stdout, stderr),
		newACPCommand(ctx, stdin, stdout, stderr),
		newVersionCommand(stdout, stderr, build),
		newPSCommand(stdout, stderr),
		newSendCommand(stdin, stderr),
		newTraceCommand(ctx, stdout, stderr),
		newStatsCommand(ctx, stdout, stderr),
		newGCCommand(ctx, stdout, stderr),
		newExportCommand(ctx, stdout, stderr),
	)
	return root
}
