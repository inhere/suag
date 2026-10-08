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
	personalprocess "github.com/inhere/suag/internal/personal/process"
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
	createdAtStartup := false
	if opts.ResumeID != "" {
		source = "resume"
		model, err := runner.LoadSession(ctx, opts.ResumeID)
		if err != nil {
			return err
		}
		sessionID = model.ID
	} else if opts.SessionID != "" {
		model, err := runner.LoadSession(ctx, opts.SessionID)
		if err == nil {
			// Opening an existing session by --session-id continues it: report resume, like claude.
			sessionID = model.ID
			source = "resume"
		} else if errors.Is(err, personal.ErrSessionNotFound) {
			model, err = runner.NewSession(ctx, opts.SessionID, "New chat")
			if err != nil {
				return err
			}
			sessionID, createdAtStartup = model.ID, true
		} else {
			return err
		}
	} else {
		model, err := runner.NewSession(ctx, "", "New chat")
		if err != nil {
			return err
		}
		sessionID, createdAtStartup = model.ID, true
	}
	fmt.Fprintln(stderr, "session id:", sessionID)
	var releaseSessionLock func() error
	var processServer *personalprocess.Server
	defer func() {
		if processServer != nil {
			_ = processServer.Close()
		}
		if releaseSessionLock != nil {
			_ = releaseSessionLock()
		}
	}()
	if sessionID != "" {
		var lockErr error
		releaseSessionLock, lockErr = runner.HoldSessionLock(sessionID)
		if lockErr != nil {
			return lockErr
		}
	}
	cwd, _ := os.Getwd()
	transcriptPath := func(id string) string {
		// With transcripts disabled no file is written, so do not advertise a path.
		if id == "" || !transcriptsEnabled() {
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
	inputSource, err := newChatInputSource(ctx, stdin, stdout)
	if err != nil {
		return err
	}
	defer inputSource.Close()
	if runner.ProcessRoot() != "" {
		processServer, err = startChatProcessRegistration(sessionID, runner.ProcessRoot(), runner, inputSource)
		if err != nil {
			return err
		}
	}
	var pendingContext string
	if hooks != nil && sessionID != "" {
		ensureTranscriptFile(transcriptPath(sessionID), sessionID)
		start := baseEvent("SessionStart")
		start.Source = source
		startResp := hooks.Call(ctx, start)
		showHookNotice(stderr, startResp)
		pendingContext = hookAdditionalContext(startResp)
	}
	var waiting chatHookWait
	// lostReply holds a web reply that arrived while the user interrupted the Stop wait.
	// It is folded into the next turn's context; if the chat ends first, warn on stderr.
	var lostReply string
	defer func() {
		// Messages queued by `suag send`/gofer that never became a turn: fail their senders
		// (exit 3 for `send --wait` style callers) and tell the person how many were dropped.
		if n := inputSource.DiscardQueued(); n > 0 {
			fmt.Fprintf(stderr, "Warning: %d queued message(s) were discarded on exit and not run.\n", n)
		}
		if waiting != nil {
			lostReply = appendContext(lostReply, waiting.Interrupt(context.Background()))
		}
		if lostReply != "" {
			fmt.Fprintln(stderr, "Warning: gofer web reply was not delivered:", truncateHookOutput(lostReply, 200))
		}
		if hooks != nil && sessionID != "" {
			hooks.Call(context.Background(), baseEvent("SessionEnd"))
		}
	}()

	fmt.Fprintln(inputSource, "Suag chat — type a task, :session, :compact or :quit.")
	stopHookActive := false
	for {
		line, hasLine, err, stopDone, injected, interrupted := nextChatInput(ctx, inputSource, waiting, &lostReply)
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
		case ":compact":
			// Compress the session context now; token figures are estimates.
			if out, err := runner.Compact(ctx, sessionID); err != nil {
				fmt.Fprintln(stderr, "Compact:", err)
			} else {
				fmt.Fprintf(inputSource, "Compacted context: ~%d -> ~%d tokens (estimate)\n", out.TokensBefore, out.TokensAfter)
			}
			continue
		}
		if createdAtStartup {
			if err := runner.SetSessionTitle(ctx, sessionID, line); err != nil {
				return err
			}
			if processServer != nil {
				if err := processServer.SetName(line); err != nil {
					return err
				}
			}
			createdAtStartup = false
		}
		if gate != nil {
			gate.Reset()
		}
		if hooks != nil {
			submit := baseEvent("UserPromptSubmit")
			submit.Prompt = personal.RedactSecretValues(line)
			// injected covers every non-keyboard source (Stop block reasons and socket
			// deliveries both flow through nextChatInput), same flag the transcript records.
			submit.Injected = injected
			submitResp := hooks.Call(ctx, submit)
			showHookNotice(stderr, submitResp)
			pendingContext = appendContext(pendingContext, hookAdditionalContext(submitResp))
		}
		if lostReply != "" {
			// A reply received during an interrupt race still reaches the model as next-turn context.
			fmt.Fprintln(stderr, "Note: delivering gofer web reply received while interrupting:", truncateHookOutput(lostReply, 200))
			pendingContext = appendContext(pendingContext, lostReply)
			lostReply = ""
		}
		turnStopActive := stopHookActive
		stopHookActive = false
		inputSource.SetBusy(true)
		if processServer != nil {
			_ = processServer.SetBusy(true)
		}
		result, runErr := runner.Run(ctx, personal.RunRequest{
			SessionID:       sessionID,
			Prompt:          line,
			SystemContext:   pendingContext,
			Injected:        injected,
			EmitToolResults: hooks != nil,
			Emit: func(eventCtx context.Context, event personal.Event) error {
				if hooks == nil || event.Kind != "tool_result" || !isShellOrExecTool(event.ToolName) {
					return nil
				}
				postTool := baseEvent("PostToolUse")
				// gofer's hookrelay only registers watches for a fixed shell-tool whitelist,
				// so suag-native names like shell_exec are normalized to "shell".
				postTool.ToolName = goferShellToolName
				postTool.ToolOutput = truncateHookOutput(personal.RedactSecretValues(event.Text), toolHookLimit)
				showHookNotice(stderr, hooks.Call(eventCtx, postTool))
				return nil
			},
		})
		inputSource.SetBusy(false)
		if processServer != nil {
			_ = processServer.SetBusy(false)
		}
		pendingContext = ""
		// Every turn end (success, error or yield) must send Stop: the gofer contract defines it as
		// "turn finished, back to waiting for input", otherwise the session stays "running" forever.
		stopMessage := ""
		if runErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fmt.Fprintln(stderr, "Error:", runErr)
			stopMessage = "Error: " + runErr.Error()
		} else if result.Status == agentcore.RunYielded {
			fmt.Fprintln(inputSource, "Waiting for more input.")
		} else {
			fmt.Fprintln(inputSource, result.Output)
			if gate != nil && gate.WasDenied() {
				fmt.Fprintln(inputSource, "Input required to authorize an MCP tool.")
			}
			stopMessage = result.Output
		}
		if hooks != nil {
			stop := baseEvent("Stop")
			stop.LastAssistant = truncateHookOutput(personal.RedactSecretValues(stopMessage), stopHookLimit)
			stop.StopHookActive = turnStopActive
			waiting = hooks.StartStop(ctx, stop)
		}
	}
}

func startChatProcessRegistration(sessionID, processRoot string, runner *personal.Runner, input *chatInputSource) (*personalprocess.Server, error) {
	model, err := runner.LoadSession(context.Background(), sessionID)
	if err != nil {
		return nil, err
	}
	cwd, _ := os.Getwd()
	return personalprocess.StartWait(processRoot, sessionID, model.Title, cwd, input.DeliverWait)
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

func nextChatInput(ctx context.Context, source *chatInputSource, waiting chatHookWait, lost *string) (string, bool, error, bool, bool, bool) {
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
			line, hasLine, err, stopResult, injected, activity := processChatInputEvent(source, event, waiting, lost)
			if activity {
				interrupted = true
				waiting = nil
				stopDone = nil
				continue
			}
			return line, hasLine, err, stopResult, injected, interrupted
		case delivery := <-source.deliveries:
			if waiting != nil {
				*lost = appendContext(*lost, waiting.Interrupt(context.Background()))
				interrupted = true
			}
			fmt.Fprintln(source, delivery.line)
			delivery.settle(nil)
			return delivery.line, true, nil, false, true, interrupted
		default:
		}
		select {
		case event := <-source.Next():
			line, hasLine, err, stopResult, injected, activity := processChatInputEvent(source, event, waiting, lost)
			if activity {
				interrupted = true
				waiting = nil
				stopDone = nil
				continue
			}
			return line, hasLine, err, stopResult, injected, interrupted
		case result, ok := <-stopDone:
			if ok && result.response.Notice != "" {
				fmt.Fprintln(source, result.response.Notice)
			}
			if !ok || result.err != nil || result.response.Decision != "block" || result.response.Reason == "" {
				fmt.Fprintln(source)
				return "", false, nil, true, false, interrupted
			}
			fmt.Fprintln(source, result.response.Reason)
			return result.response.Reason, true, nil, true, true, interrupted
		case delivery := <-source.deliveries:
			if waiting != nil {
				*lost = appendContext(*lost, waiting.Interrupt(context.Background()))
				interrupted = true
			}
			fmt.Fprintln(source, delivery.line)
			delivery.settle(nil)
			return delivery.line, true, nil, false, delivery.injected, interrupted
		case <-ctx.Done():
			return "", false, ctx.Err(), false, false, interrupted
		}
	}
}

func processChatInputEvent(source *chatInputSource, event chatInputEvent, waiting chatHookWait, lost *string) (string, bool, error, bool, bool, bool) {
	if event.activity {
		if waiting != nil {
			*lost = appendContext(*lost, waiting.Interrupt(context.Background()))
		}
		source.AckActivity()
		return "", false, nil, false, false, true
	}
	if event.err != nil && waiting != nil {
		*lost = appendContext(*lost, waiting.Interrupt(context.Background()))
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

// goferShellToolName is the tool name gofer recognizes as a shell tool.
const goferShellToolName = "shell"

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

// showHookNotice prints the server-provided notice (if any) for the person at the terminal.
// It always goes to stderr so it never mixes into the conversation output.
func showHookNotice(w io.Writer, response goferHookResponse) {
	if response.Notice != "" {
		fmt.Fprintln(w, response.Notice)
	}
}
