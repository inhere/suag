package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	gkconfig "github.com/gookit/config/v2"
	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/pkg/agentcore"
)

const (
	goferReplyPrefix = "[gofer web 回复] "
	toolHookLimit    = 8 * 1024
	stopHookLimit    = 16 * 1024
)

func runPersonalChat(ctx context.Context, runner *personal.Runner, opts personalOptions, stdin io.Reader, stdout, stderr io.Writer, gates ...*cliMCPGate) error {
	return runPersonalChatWithHooks(ctx, runner, opts, stdin, stdout, stderr, newGoferHooks(), gates...)
}

func runPersonalChatWithHooks(ctx context.Context, runner *personal.Runner, opts personalOptions, stdin io.Reader, stdout, stderr io.Writer, hooks chatHookClient, gates ...*cliMCPGate) error {
	var gate *cliMCPGate
	if len(gates) > 0 {
		gate = gates[0]
	}
	sessionID := ""
	source := "startup"
	if opts.ResumeID != "" {
		source = "resume"
	}
	if opts.SessionID != "" || opts.ResumeID != "" {
		var err error
		sessionID, err = openRequestedSession(ctx, runner, opts)
		if err != nil {
			return err
		}
	}
	cwd, _ := os.Getwd()
	transcriptPath := func(id string) string {
		if id == "" {
			return ""
		}
		home, err := appconfig.ResolveSuagHome()
		if err != nil {
			return ""
		}
		return filepath.Join(home, "transcripts", id+".jsonl")
	}
	baseEvent := func(name string) goferHookEvent {
		return goferHookEvent{Name: name, SessionID: sessionID, CWD: cwd, TranscriptPath: transcriptPath(sessionID)}
	}
	var pendingContext string
	if hooks != nil && sessionID != "" {
		ensureTranscriptFile(transcriptPath(sessionID), sessionID)
		start := baseEvent("SessionStart")
		start.Source = source
		pendingContext = hookAdditionalContext(hooks.Call(ctx, start))
	}
	inputSource, err := newChatInputSource(ctx, stdin, stdout)
	if err != nil {
		return err
	}
	defer inputSource.Close()
	var waiting chatHookWait
	defer func() {
		if waiting != nil {
			waiting.Interrupt(context.Background())
		}
		if hooks != nil && sessionID != "" {
			hooks.Call(context.Background(), baseEvent("SessionEnd"))
		}
	}()

	fmt.Fprintln(inputSource, "Suag chat — type a task, :session or :quit.")
	stopHookActive := false
	for {
		line, hasLine, err, stopDone, injected, interrupted := nextChatInput(ctx, inputSource, waiting)
		if interrupted {
			waiting = nil
		} else if waiting != nil && stopDone {
			waiting = nil
			if injected {
				stopHookActive = true
			}
		}
		if stopDone && !injected {
			continue
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if !hasLine {
			return nil
		}
		line = strings.TrimSpace(line)
		switch line {
		case "":
			continue
		case ":quit":
			return nil
		case ":session":
			fmt.Fprintln(inputSource, "session id:", sessionID)
			continue
		}
		if sessionID == "" {
			model, err := runner.NewSession(ctx, "", line)
			if err != nil {
				return err
			}
			sessionID = model.SessionID
			fmt.Fprintln(stderr, "session id:", sessionID)
			if hooks != nil {
				ensureTranscriptFile(transcriptPath(sessionID), sessionID)
				start := baseEvent("SessionStart")
				start.Source = source
				pendingContext = appendContext(pendingContext, hookAdditionalContext(hooks.Call(ctx, start)))
			}
		}
		if gate != nil {
			gate.Reset()
		}
		if hooks != nil {
			submit := baseEvent("UserPromptSubmit")
			submit.Prompt = personal.RedactSecretValues(line)
			pendingContext = appendContext(pendingContext, hookAdditionalContext(hooks.Call(ctx, submit)))
		}
		turnStopActive := stopHookActive
		stopHookActive = false
		result, runErr := runner.Run(ctx, personal.RunRequest{
			SessionID:       sessionID,
			Prompt:          line,
			SystemContext:   pendingContext,
			EmitToolResults: hooks != nil,
			Emit: func(eventCtx context.Context, event personal.Event) error {
				if hooks == nil || event.Kind != "tool_result" || !isShellOrExecTool(event.ToolName) {
					return nil
				}
				postTool := baseEvent("PostToolUse")
				postTool.ToolName = event.ToolName
				postTool.ToolOutput = truncateHookOutput(personal.RedactSecretValues(event.Text), toolHookLimit)
				hooks.Call(eventCtx, postTool)
				return nil
			},
		})
		pendingContext = ""
		if runErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fmt.Fprintln(stderr, "Error:", runErr)
			continue
		}
		if result.Status == agentcore.RunYielded {
			fmt.Fprintln(inputSource, "Waiting for more input.")
			continue
		}
		fmt.Fprintln(inputSource, result.Output)
		if gate != nil && gate.WasDenied() {
			fmt.Fprintln(inputSource, "Input required to authorize an MCP tool.")
		}
		if hooks != nil {
			stop := baseEvent("Stop")
			stop.LastAssistant = truncateHookOutput(personal.RedactSecretValues(result.Output), stopHookLimit)
			stop.StopHookActive = turnStopActive
			waiting = hooks.StartStop(ctx, stop)
		}
	}
}

func ensureTranscriptFile(path, sessionID string) {
	if path == "" || !transcriptsEnabled() {
		return
	}
	transcript, err := personal.OpenTranscript(filepath.Dir(path), sessionID)
	if err != nil {
		return
	}
	_ = transcript.Close()
}

func nextChatInput(ctx context.Context, source *chatInputSource, waiting chatHookWait) (string, bool, error, bool, bool, bool) {
	prompt := "> "
	if waiting != nil {
		prompt = "> (web 可回复) "
	}
	if !source.Request(prompt) {
		return "", false, ctx.Err(), false, false, false
	}
	var stopDone <-chan goferHookResult
	if waiting != nil {
		stopDone = waiting.Done()
	}
	interrupted := false
	for {
		// Prefer a byte already observed by the reader over a simultaneous Stop result.
		select {
		case event := <-source.Next():
			line, hasLine, err, stopResult, injected, activity := processChatInputEvent(source, event, waiting)
			if activity {
				interrupted = true
				waiting = nil
				stopDone = nil
				continue
			}
			return line, hasLine, err, stopResult, injected, interrupted
		default:
		}
		select {
		case event := <-source.Next():
			line, hasLine, err, stopResult, injected, activity := processChatInputEvent(source, event, waiting)
			if activity {
				interrupted = true
				waiting = nil
				stopDone = nil
				continue
			}
			return line, hasLine, err, stopResult, injected, interrupted
		case result, ok := <-stopDone:
			if !ok || result.err != nil || result.response.Decision != "block" || result.response.Reason == "" {
				fmt.Fprintln(source)
				return "", false, nil, true, false, interrupted
			}
			fmt.Fprintln(source, result.response.Reason)
			return result.response.Reason, true, nil, true, true, interrupted
		case <-ctx.Done():
			return "", false, ctx.Err(), false, false, interrupted
		}
	}
}

func processChatInputEvent(source *chatInputSource, event chatInputEvent, waiting chatHookWait) (string, bool, error, bool, bool, bool) {
	if event.activity {
		if waiting != nil {
			waiting.Interrupt(context.Background())
		}
		source.AckActivity()
		return "", false, nil, false, false, true
	}
	if event.err != nil && waiting != nil {
		waiting.Interrupt(context.Background())
		return event.line, false, event.err, false, false, true
	}
	source.CompleteLine()
	return event.line, event.err == nil, event.err, false, false, false
}

func appendContext(current, next string) string {
	if next == "" {
		return current
	}
	if current == "" {
		return next
	}
	return current + "\n\n" + next
}

func isShellOrExecTool(name string) bool {
	name = strings.ToLower(name)
	return strings.Contains(name, "shell") || strings.Contains(name, "exec")
}

func transcriptsEnabled() bool {
	value := gkconfig.Get("transcripts")
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return !strings.EqualFold(strings.TrimSpace(v), "off") && !strings.EqualFold(strings.TrimSpace(v), "false")
	default:
		return true
	}
}
