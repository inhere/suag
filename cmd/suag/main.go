// Command suag runs the personal agent through CLI or ACP stdio.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gookit/goutil/cflag"
	"github.com/gookit/goutil/cflag/capp"
	"github.com/inhere/suag/internal/app"
	appconfig "github.com/inhere/suag/internal/config"
	personalprocess "github.com/inhere/suag/internal/personal/process"
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
		os.Exit(commandExitCode(err))
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
	var psJSON bool
	ps := capp.NewCmd("ps", "List local Suag chat processes", func(c *capp.Cmd) error {
		if len(c.RemainArgs()) != 0 {
			return fmt.Errorf("suag ps does not accept arguments")
		}
		root, err := localProcessRoot()
		if err != nil {
			return err
		}
		records, err := personalprocess.List(root)
		if err != nil {
			return err
		}
		if psJSON {
			return json.NewEncoder(stdout).Encode(records)
		}
		for _, record := range records {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%d\t%s\n", record.SessionID, record.Name, record.CWD, record.State, record.PID, record.StartedAt.Format(time.RFC3339))
		}
		return nil
	})
	ps.SetOutput(stderr)
	ps.BoolVar(&psJSON, "json", false, "write machine-readable JSON")
	var sendSession, sendText string
	send := capp.NewCmd("send", "Send text to a local Suag chat process", func(c *capp.Cmd) error {
		if len(c.RemainArgs()) != 0 {
			return fmt.Errorf("suag send does not accept arguments")
		}
		if strings.TrimSpace(sendSession) == "" {
			return fmt.Errorf("--session is required")
		}
		text := sendText
		if text == "" {
			// Read one byte past the cap so oversize input is rejected, never truncated.
			data, err := io.ReadAll(io.LimitReader(stdin, maxSendStdinBytes+1))
			if err != nil {
				return err
			}
			if len(data) > maxSendStdinBytes {
				return processExitError{code: 4, err: fmt.Errorf("stdin exceeds the %d byte (1 MiB) limit; nothing was sent", maxSendStdinBytes)}
			}
			text = string(data)
		}
		root, err := localProcessRoot()
		if err != nil {
			return err
		}
		records, err := personalprocess.List(root)
		if err != nil {
			return err
		}
		record, err := personalprocess.Resolve(records, sendSession)
		if errors.Is(err, personalprocess.ErrNotOnline) {
			return processExitError{code: 3, err: err}
		}
		if errors.Is(err, personalprocess.ErrAmbiguous) {
			return processExitError{code: 2, err: err}
		}
		if err != nil {
			return err
		}
		response, err := personalprocess.Send(record, personalprocess.Request{Text: text, From: "cli"})
		if errors.Is(err, personalprocess.ErrNotOnline) {
			return processExitError{code: 3, err: err}
		}
		if err != nil {
			return err
		}
		if response.Queued {
			fmt.Fprintln(stderr, "queued")
		} else {
			fmt.Fprintln(stderr, "delivered")
		}
		return nil
	})
	send.SetOutput(stderr)
	send.StringVar(&sendSession, "session", "", "session ID, prefix, or name")
	send.StringVar(&sendText, "text", "", "message text; defaults to stdin")
	cli.Add(
		app.NewPersonalCommand(ctx, "run", stdin, stdout, stderr),
		app.NewPersonalCommand(ctx, "chat", stdin, stdout, stderr),
		acp,
		version,
		ps,
		send,
	)
	return cli
}

// maxSendStdinBytes caps `suag send` stdin; exit codes: 2 ambiguous, 3 offline, 4 too large.
const maxSendStdinBytes = 1 << 20

type processExitError struct {
	code int
	err  error
}

func (e processExitError) Error() string { return e.err.Error() }
func (e processExitError) Unwrap() error { return e.err }
func (e processExitError) ExitCode() int { return e.code }

func commandExitCode(err error) int {
	var exitCoder interface{ ExitCode() int }
	if errors.As(err, &exitCoder) {
		return exitCoder.ExitCode()
	}
	return 1
}

func localProcessRoot() (string, error) {
	home, err := appconfig.ResolveSuagHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "run"), nil
}
