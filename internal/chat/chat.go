package chat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	appconfig "github.com/inhere/suag/internal/config"
	suaghooks "github.com/inhere/suag/internal/hooks"
	"github.com/inhere/suag/internal/personal"
	personalprocess "github.com/inhere/suag/internal/personal/process"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/pkg/agentcore"
)

const (
	toolHookLimit = 8 * 1024
	stopHookLimit = 16 * 1024
)

// Options selects which session a chat opens: a new one, an existing one, or a
// handoff child of another session.
type Options struct {
	SessionID string
	ResumeID  string
	// FromID starts a NEW session that inherits state from this session.
	FromID string
	// Asker is where the chat approver asks before running a tool. Run binds
	// the terminal input to it only when stdin is a terminal; left unbound,
	// every tool that needs a confirmation is refused. May be nil.
	Asker *tools.BindableAsker
}

// Run drives the interactive chat REPL on top of runner; hooks may be nil.
func Run(ctx context.Context, runner *personal.Runner, opts Options, stdin io.Reader, stdout, stderr io.Writer, hooks suaghooks.Client) error {
	sessionID := ""
	source := "startup"
	createdAtStartup := false
	if opts.FromID != "" {
		model, err := runner.Handoff(ctx, personal.HandoffRequest{SourceID: opts.FromID, NewID: opts.SessionID, Title: "New chat"})
		if err != nil {
			return personal.SessionNotFound(err, opts.FromID)
		}
		sessionID, createdAtStartup = model.ID, true
	} else if opts.ResumeID != "" {
		source = "resume"
		model, err := runner.LoadSession(ctx, opts.ResumeID)
		if err != nil {
			return personal.SessionNotFound(err, opts.ResumeID)
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
	// Registered first so it runs last, after the terminal is restored: how to
	// continue this session and what this chat process used.
	chatStart := time.Now()
	var stats chatStats
	defer func() { stats.print(stderr, sessionID, time.Since(chatStart)) }()
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
		if id == "" || !appconfig.TranscriptsEnabled() {
			return ""
		}
		home, err := appconfig.ResolveSuagHome()
		if err != nil {
			return ""
		}
		return filepath.Join(home, "transcripts", id+".jsonl")
	}
	baseEvent := func(name string) suaghooks.Event {
		return suaghooks.Event{Name: name, SessionID: sessionID, CWD: cwd, TranscriptPath: transcriptPath(sessionID)}
	}
	inputSource, err := newChatInputSource(ctx, stdin, stdout)
	if err != nil {
		return err
	}
	defer inputSource.Close()
	// All conversation output goes through the renderer (and the input source,
	// which keeps the line editor's screen state consistent).
	render := newRenderer(rendererOptions{
		Out: inputSource, Err: stderr,
		Color: colorEnabledFor(stdout), ErrColor: colorEnabledFor(stderr),
		Provider: runner.ProviderName(), Model: runner.Model(),
	})
	if opts.Asker != nil && inputSource.terminal != nil {
		opts.Asker.Bind(chatAsker{source: inputSource, render: render})
		defer opts.Asker.Bind(nil)
	}
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
		pendingContext = suaghooks.AdditionalContext(startResp)
	}
	var waiting suaghooks.Wait
	// lostReply holds a web reply that arrived while the user interrupted the Stop wait.
	// It is folded into the next turn's context; if the chat ends first, warn on stderr.
	var lostReply string
	defer func() {
		// Messages queued by `suag send` that never became a turn: fail their senders
		// (exit 3 for `send --wait` style callers) and tell the person how many were dropped.
		if n := inputSource.DiscardQueued(); n > 0 {
			fmt.Fprintf(stderr, "Warning: %d queued message(s) were discarded on exit and not run.\n", n)
		}
		if waiting != nil {
			lostReply = appendContext(lostReply, waiting.Interrupt(context.Background()))
		}
		if lostReply != "" {
			fmt.Fprintln(stderr, "Warning: hook reply was not delivered:", suaghooks.TruncateOutput(lostReply, 200))
		}
		if hooks != nil && sessionID != "" {
			hooks.Call(context.Background(), baseEvent("SessionEnd"))
		}
	}()

	render.Banner()
	stopHookActive := false
	for {
		line, hasLine, err, stopDone, injected, interrupted := nextChatInput(ctx, inputSource, render, waiting, &lostReply)
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
		if hooks != nil {
			submit := baseEvent("UserPromptSubmit")
			submit.Prompt = personal.RedactSecretValues(line)
			// injected covers every non-keyboard source (Stop block reasons and socket
			// deliveries both flow through nextChatInput), same flag the transcript records.
			submit.Injected = injected
			submitResp := hooks.Call(ctx, submit)
			showHookNotice(stderr, submitResp)
			pendingContext = appendContext(pendingContext, suaghooks.AdditionalContext(submitResp))
		}
		if lostReply != "" {
			// A reply received during an interrupt race still reaches the model as next-turn context.
			fmt.Fprintln(stderr, "Note: delivering hook reply received while interrupting:", suaghooks.TruncateOutput(lostReply, 200))
			pendingContext = appendContext(pendingContext, lostReply)
			lostReply = ""
		}
		turnStopActive := stopHookActive
		stopHookActive = false
		inputSource.SetBusy(true)
		if processServer != nil {
			_ = processServer.SetBusy(true)
		}
		render.BeginTurn()
		turnStart := time.Now()
		result, runErr := runner.Run(ctx, personal.RunRequest{
			SessionID:     sessionID,
			Prompt:        line,
			SystemContext: pendingContext,
			Injected:      injected,
			Emit: func(eventCtx context.Context, event personal.Event) error {
				render.Handle(event)
				if hooks == nil || event.Kind != "tool_result" {
					return nil
				}
				// hooks.post_tool_use decides which tools send PostToolUse and under which name.
				toolName, send := hooks.PostToolName(event.ToolName)
				if !send {
					return nil
				}
				postTool := baseEvent("PostToolUse")
				postTool.ToolName = toolName
				postTool.ToolOutput = suaghooks.TruncateOutput(personal.RedactSecretValues(event.Text), toolHookLimit)
				showHookNotice(stderr, hooks.Call(eventCtx, postTool))
				return nil
			},
		})
		inputSource.SetBusy(false)
		if processServer != nil {
			_ = processServer.SetBusy(false)
		}
		stats.add(result)
		pendingContext = ""
		// Every turn end (success, error or yield) must send Stop: the hook contract defines it as
		// "turn finished, back to waiting for input", otherwise a hook-side session stays "running" forever.
		stopMessage := ""
		if runErr != nil {
			render.Finish()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(runErr, tools.ErrAborted) {
				// Ctrl-C/Ctrl-D at an approval question: only this turn ends.
				render.Notice("已取消")
			} else {
				render.Error(runErr)
			}
			stopMessage = "Error: " + runErr.Error()
		} else if result.Status == agentcore.RunYielded {
			render.Finish()
			render.Notice("Waiting for more input.")
			render.Status(result, time.Since(turnStart))
		} else {
			// Streamed text is already on screen; otherwise print the answer now.
			if !render.Finish() {
				render.Answer(result.Output)
			}
			if len(result.DeniedTools) > 0 {
				render.Notice("本轮被拒绝的工具：" + strings.Join(result.DeniedTools, ", "))
			}
			render.Status(result, time.Since(turnStart))
			stopMessage = result.Output
		}
		if hooks != nil {
			stop := baseEvent("Stop")
			stop.LastAssistant = suaghooks.TruncateOutput(personal.RedactSecretValues(stopMessage), stopHookLimit)
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
	if path == "" || !appconfig.TranscriptsEnabled() {
		return
	}
	transcript, err := personal.OpenTranscript(filepath.Dir(path), sessionID)
	if err != nil {
		return
	}
	_ = transcript.Close()
}

func nextChatInput(ctx context.Context, source *chatInputSource, render *Renderer, waiting suaghooks.Wait, lost *string) (string, bool, error, bool, bool, bool) {
	if !source.Request(render.Prompt(waiting != nil)) {
		return "", false, ctx.Err(), false, false, false
	}
	var stopDone <-chan suaghooks.Result
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
			render.External(delivery.line)
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
			if ok && result.Response.Notice != "" {
				fmt.Fprintln(source, result.Response.Notice)
			}
			if !ok || result.Err != nil || result.Response.Decision != "block" || result.Response.Reason == "" {
				fmt.Fprintln(source)
				return "", false, nil, true, false, interrupted
			}
			render.External(result.Response.Reason)
			return result.Response.Reason, true, nil, true, true, interrupted
		case delivery := <-source.deliveries:
			if waiting != nil {
				*lost = appendContext(*lost, waiting.Interrupt(context.Background()))
				interrupted = true
			}
			render.External(delivery.line)
			delivery.settle(nil)
			return delivery.line, true, nil, false, delivery.injected, interrupted
		case <-ctx.Done():
			return "", false, ctx.Err(), false, false, interrupted
		}
	}
}

func processChatInputEvent(source *chatInputSource, event chatInputEvent, waiting suaghooks.Wait, lost *string) (string, bool, error, bool, bool, bool) {
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

// showHookNotice prints the server-provided notice (if any) for the person at the terminal.
// It always goes to stderr so it never mixes into the conversation output.
func showHookNotice(w io.Writer, response suaghooks.Response) {
	if response.Notice != "" {
		fmt.Fprintln(w, response.Notice)
	}
}

// chatStats accumulates what one chat process used, for the exit summary.
type chatStats struct {
	turns   int
	usage   agentcore.Usage
	session agentcore.Usage // session-cumulative usage after the last turn
}

func (s *chatStats) add(result personal.RunResult) {
	s.turns++
	s.usage.InputTokens += result.Usage.InputTokens
	s.usage.OutputTokens += result.Usage.OutputTokens
	if result.SessionUsage.TotalTokens > 0 || result.SessionUsage.InputTokens > 0 {
		s.session = result.SessionUsage
	}
}

// print writes the exit summary: the resume command, this process's turns and
// tokens (plus the session total when known) and how long chat ran.
func (s chatStats) print(w io.Writer, sessionID string, elapsed time.Duration) {
	if sessionID == "" {
		return
	}
	line := fmt.Sprintf("本次 %d 轮 · in %d / out %d tokens", s.turns, s.usage.InputTokens, s.usage.OutputTokens)
	if s.session.InputTokens > 0 || s.session.OutputTokens > 0 {
		line += fmt.Sprintf("（会话累计 in %d / out %d）", s.session.InputTokens, s.session.OutputTokens)
	}
	fmt.Fprintf(w, "%s · 运行 %s\n续接：suag chat --resume %s\n", line, formatElapsed(elapsed), sessionID)
}
