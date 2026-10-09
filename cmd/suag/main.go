// Command suag runs the personal agent through CLI or ACP stdio.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/inhere/suag/internal/cli"
	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/wiring"
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
	wiring.SetupLogging()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := cli.New(ctx, os.Stdin, os.Stdout, os.Stderr, cli.Build{Version: Version, VersionLine: formatVersion(Version, GitCommit)}).RunWithArgs(args); err != nil {
		msg, code := exitReport(ctx, err)
		fmt.Fprintln(os.Stderr, msg)
		os.Exit(code)
	}
}

// exitReport turns the command error into the stderr line and exit code. An
// error caused by Ctrl-C/SIGTERM (ctx canceled by the signal) reads "已中断"
// instead of "context canceled" and exits 130, the shell convention for SIGINT.
func exitReport(ctx context.Context, err error) (string, int) {
	if ctx.Err() != nil && errors.Is(err, context.Canceled) {
		return "已中断", 130
	}
	return err.Error(), cli.ExitCode(err)
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
