package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/pkg/agentcore"
)

func runPersonalChat(ctx context.Context, runner *personal.Runner, opts personalOptions, stdin io.Reader, stdout, stderr io.Writer, gates ...*cliMCPGate) error {
	var gate *cliMCPGate
	if len(gates) > 0 {
		gate = gates[0]
	}
	sessionID := ""
	if opts.SessionID != "" || opts.ResumeID != "" {
		var err error
		sessionID, err = openRequestedSession(ctx, runner, opts)
		if err != nil {
			return err
		}
	}
	fmt.Fprintln(stdout, "Suag chat — type a task, :session or :quit.")
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for {
		if _, err := fmt.Fprint(stdout, "> "); err != nil {
			return err
		}
		if !scanner.Scan() {
			return scanner.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		switch line {
		case "":
			continue
		case ":quit":
			return nil
		case ":session":
			fmt.Fprintln(stdout, "session id:", sessionID)
			continue
		}
		if sessionID == "" {
			model, err := runner.NewSession(ctx, "", line)
			if err != nil {
				return err
			}
			sessionID = model.SessionID
			fmt.Fprintln(stderr, "session id:", sessionID)
		}
		if gate != nil {
			gate.Reset()
		}
		result, err := runner.Run(ctx, personal.RunRequest{SessionID: sessionID, Prompt: line})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fmt.Fprintln(stderr, "Error:", err)
			continue
		}
		if result.Status == agentcore.RunYielded {
			fmt.Fprintln(stdout, "Waiting for more input.")
			continue
		}
		fmt.Fprintln(stdout, result.Output)
		if gate != nil && gate.WasDenied() {
			fmt.Fprintln(stdout, "Input required to authorize an MCP tool.")
		}
	}
}
