// Command suag runs the personal agent in CLI mode or the legacy Web server.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/inhere/suag/internal/app"
	appconfig "github.com/inhere/suag/internal/config"
)

func main() {
	if err := appconfig.Load(); err != nil {
		fmt.Fprintln(os.Stderr, "load config:", err)
		os.Exit(1)
	}
	app.SetupLogging()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	mode := "chat"
	var modeArgs []string
	if len(os.Args) > 1 {
		mode, modeArgs = os.Args[1], os.Args[2:]
	}
	if mode == "run" || mode == "chat" {
		if err := app.RunPersonalCLI(ctx, mode, modeArgs, os.Stdin, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	runtimeApp, err := app.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build app: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := runtimeApp.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to close app: %v\n", err)
		}
	}()

	if err := runtimeApp.Run(ctx, mode); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		if errors.Is(err, app.ErrUnknownMode) {
			fmt.Fprintln(os.Stderr, "Usage: suag [run|chat|server]")
		}
		os.Exit(1)
	}
}
