// Command suag runs the personal agent through CLI or ACP stdio.
package main

import (
	"context"
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
	if mode == "acp" {
		if len(modeArgs) != 0 {
			fmt.Fprintln(os.Stderr, "suag acp does not accept arguments")
			os.Exit(1)
		}
		if err := app.RunPersonalACP(ctx, os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "acp:", err)
			os.Exit(1)
		}
		return
	}

	fmt.Fprintf(os.Stderr, "unknown mode %q\nUsage: suag [run|chat|acp]\n", mode)
	os.Exit(1)
}
