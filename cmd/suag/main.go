// Command suag is the main entry point for the suag AI agent orchestration engine.
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
	_ = appconfig.Load()
	app.SetupLogging()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

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

	// Determine run mode from args.
	mode := "chat"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	if err := runtimeApp.Run(ctx, mode); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		if errors.Is(err, app.ErrUnknownMode) {
			fmt.Fprintln(os.Stderr, "Usage: suag [chat|server]")
		}
		os.Exit(1)
	}
}
