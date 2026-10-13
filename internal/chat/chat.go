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

	"github.com/inhere/suga/internal/chat/lineedit"
	appconfig "github.com/inhere/suga/internal/config"
	sugahooks "github.com/inhere/suga/internal/hooks"
	"github.com/inhere/suga/internal/personal"
	personalprocess "github.com/inhere/suga/internal/personal/process"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/redact"
	"github.com/inhere/suga/pkg/agentcore/schedule"
	"github.com/inhere/suga/pkg/agentcore/tasks"
	"github.com/inhere/suga/pkg/agentcore/tools"
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
	// WriteTools tells the banner whether file_write/shell_exec are available.
	WriteTools bool
	// Markdown renders the model's text as terminal markdown (config
	// chat.markdown, on by default). Only a colored terminal uses it: a pipe,
	// a redirected output or NO_COLOR keeps the raw text.
	Markdown bool
	// Spinner shows the "思考中…" waiting line while the model is asked for its
	// next message (config chat.spinner, on by default). Like Markdown it needs
	// a colored terminal: a pipe, a redirected output or NO_COLOR keeps it off.
	Spinner bool
	// Thinking (the effective thinking level, "" = provider default) and
	// ContextWindow (tokens, 0 = unknown) are shown in the banner.
	Thinking      string
	ContextWindow int
	// IdleAfter and IdleMinRatio configure the idle consolidation
	// (context.idle_after, context.idle_min_ratio); IdleAfter 0 turns it off.
	IdleAfter    time.Duration
	IdleMinRatio float64
	// Tasks are the background tasks of this chat (nil: none); TaskWake
	// signals a task end that should start a turn when chat is idle
	// (tasks.wake_on_exit; nil: never).
	Tasks    *tasks.Manager
	TaskWake <-chan struct{}
	// StopBackground runs each Stop hook as a background task of Tasks
	// (hooks.stop_background): typing does not interrupt it and a block
	// reply arrives as a task notice. Needs Tasks.
	StopBackground bool
	// scheduleOptions are extra scheduler options (tests inject a fake clock);
	// scheduleObserver sees every scheduler event synchronously (tests).
	scheduleOptions  []schedule.Option
	scheduleObserver func(schedule.Event)
}

// Run drives the interactive chat REPL on top of runner; hooks may be nil.
func Run(ctx context.Context, runner *personal.Runner, opts Options, stdin io.Reader, stdout, stderr io.Writer, hooks sugahooks.Client) error {
	hooks = recordHooks(hooks, runner)
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
		home, err := appconfig.ResolveSugaHome()
		if err != nil {
			return ""
		}
		return filepath.Join(home, "transcripts", id+".jsonl")
	}
	baseEvent := func(name string) sugahooks.Event {
		return sugahooks.Event{Name: name, SessionID: sessionID, CWD: cwd, TranscriptPath: transcriptPath(sessionID)}
	}
	inputSource, err := newChatInputSource(ctx, stdin, stdout)
	if err == nil && opts.Tasks != nil {
		inputSource.taskWake = opts.TaskWake
		inputSource.taskNotice = func() string {
			infos := opts.Tasks.TakeNotices(sessionID)
			if len(infos) == 0 {
				return ""
			}
			return personal.TaskNoticeText(infos)
		}
	}
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
		Thinking: opts.Thinking, ContextWindow: opts.ContextWindow,
		WriteTools: opts.WriteTools, Markdown: opts.Markdown, Spinner: opts.Spinner,
	})
	// The waiting line must never outlive the chat: a frame goroutine left
	// behind would keep writing over the exit summary.
	defer render.stopThinking()
	// Background idle consolidation; closed before the input source, so its
	// last status line still goes through it.
	idle := newIdleConsolidation(runner, sessionID, opts.IdleAfter, opts.IdleMinRatio, render, opts.scheduleOptions,
		opts.scheduleObserver)
	defer idle.Close()
	if opts.Asker != nil && inputSource.terminal != nil {
		opts.Asker.Bind(chatAsker{source: inputSource, render: render})
		defer opts.Asker.Bind(nil)
	}
	if runner.ProcessRoot() != "" {
		processServer, err = startChatProcessRegistration(sessionID, runner.ProcessRoot(), runner, inputSource)
		if err != nil {
			// Registration only adds the local session socket (`suga send`, gofer delivery);
			// losing it must not end the chat. The common cause is a SUGA_HOME deep enough
			// that <root>/<pid>.sock exceeds the platform's socket path limit.
			processServer = nil
			fmt.Fprintf(stderr, "提示：本会话未注册本地会话 socket（%v），因此收不到 suga send 与 gofer 投递；建议缩短 SUGA_HOME 路径。聊天继续运行。\n", err)
		}
	}
	var pendingContext string
	if hooks != nil && sessionID != "" {
		ensureTranscriptFile(transcriptPath(sessionID), sessionID)
		start := baseEvent("SessionStart")
		start.Source = source
		startResp := hooks.Call(ctx, start)
		showHookNotice(stderr, startResp)
		pendingContext = sugahooks.AdditionalContext(startResp)
	}
	var waiting sugahooks.Wait
	var bgStop *backgroundStop
	if hooks != nil && opts.StopBackground && opts.Tasks != nil && sessionID != "" {
		bgStop = &backgroundStop{hooks: hooks, tasks: opts.Tasks, sessionID: sessionID}
	}
	// lostReply holds a web reply that arrived while the user interrupted the Stop wait.
	// It is folded into the next turn's context; if the chat ends first, warn on stderr.
	var lostReply string
	defer func() {
		// Messages queued by `suga send` that never became a turn: fail their senders
		// (exit 3 for `send --wait` style callers) and tell the person how many were dropped.
		if n := inputSource.DiscardQueued(); n > 0 {
			fmt.Fprintf(stderr, "Warning: %d queued message(s) were discarded on exit and not run.\n", n)
		}
		if waiting != nil {
			lostReply = appendContext(lostReply, waiting.Interrupt(context.Background()))
		}
		if lostReply != "" {
			fmt.Fprintln(stderr, "Warning: hook reply was not delivered:", sugahooks.TruncateOutput(lostReply, 200))
		}
		if hooks != nil && sessionID != "" {
			hooks.Call(context.Background(), baseEvent("SessionEnd"))
		}
	}()

	render.Banner()
	stopHookActive := false
	var lastInterrupt time.Time
	for {
		in, err := nextChatInput(ctx, inputSource, render, waiting, &lostReply)
		line, hasLine, stopDone, injected, interrupted := in.line, in.hasLine, in.stopDone, in.injected, in.interrupted
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
			// Ctrl-C at the prompt exits only when pressed twice in a row
			// within exitConfirmWindow, so a stray one does not end the chat.
			// Ctrl-D (EOF) still exits at once.
			if errors.Is(err, lineedit.ErrInterrupt) && time.Since(lastInterrupt) > exitConfirmWindow {
				lastInterrupt = time.Now()
				if inputSource.terminal != nil {
					inputSource.terminal.AbortLine()
				}
				render.Hint("再按一次 Ctrl+C 退出（Ctrl+D 直接退出）")
				continue
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		lastInterrupt = time.Time{}
		if !hasLine {
			return nil
		}
		line = strings.TrimSpace(line)
		// Any input (keyboard, delivery, Stop hook reply) is activity: the idle
		// clock starts over. A running consolidation is preempted by the
		// Run/Compact below through the session gate.
		idle.Touch()
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
			idle.Pause()
			if out, err := runner.Compact(ctx, sessionID); err != nil {
				fmt.Fprintln(stderr, "Compact:", err)
			} else {
				idle.Compacted()
				fmt.Fprintf(inputSource, "Compacted context: ~%d -> ~%d tokens (estimate)\n", out.TokensBefore, out.TokensAfter)
			}
			idle.Resume()
			continue
		}
		// A turn starts: no idle consolidation until it has ended (EndTurn).
		idle.Pause()
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
			submit.Prompt = redact.Secrets(line)
			// injected covers every non-keyboard source (Stop block reasons and socket
			// deliveries both flow through nextChatInput), same flag the transcript records.
			submit.Injected = injected
			submitResp := hooks.Call(ctx, submit)
			showHookNotice(stderr, submitResp)
			pendingContext = appendContext(pendingContext, sugahooks.AdditionalContext(submitResp))
		}
		if lostReply != "" {
			// A reply received during an interrupt race still reaches the model as next-turn context.
			fmt.Fprintln(stderr, "Note: delivering hook reply received while interrupting:", sugahooks.TruncateOutput(lostReply, 200))
			pendingContext = appendContext(pendingContext, lostReply)
			lostReply = ""
		}
		// A background Stop that asked to continue makes this turn its follow-up.
		turnStopActive := stopHookActive || bgStop.takeBlocked()
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
				postTool.ToolOutput = sugahooks.TruncateOutput(redact.Secrets(event.Text), toolHookLimit)
				// The notice goes to stderr while the waiting line sits on stdout: take
				// that line down (the tool result above started a new one) so the two
				// never end up on the same screen line, and put it back afterwards.
				render.stopThinking()
				showHookNotice(stderr, hooks.Call(eventCtx, postTool))
				render.startThinking()
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
			stop.LastAssistant = sugahooks.TruncateOutput(redact.Secrets(stopMessage), stopHookLimit)
			stop.StopHookActive = turnStopActive
			if bgStop == nil {
				waiting = hooks.StartStop(ctx, stop)
			} else if err := bgStop.start(ctx, stop); err != nil {
				// No task slot (tasks.max_running): wait at the prompt as before.
				render.Notice("Stop hook 未能在后台运行，改为等待：" + err.Error())
				waiting = hooks.StartStop(ctx, stop)
			}
		}
		// The turn is over (Stop sent): the idle clock runs from now.
		idle.EndTurn(result)
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

// chatInput is one result of nextChatInput: a line to run (hasLine), or the
// end of a Stop hook wait (stopDone; injected when the hook supplied the next
// prompt). interrupted reports that typing or a delivery cut a Stop wait short.
type chatInput struct {
	line                                     string
	hasLine, stopDone, injected, interrupted bool
}

func nextChatInput(ctx context.Context, source *chatInputSource, render *Renderer, waiting sugahooks.Wait, lost *string) (chatInput, error) {
	if !source.Request(render.Prompt(waiting != nil)) {
		return chatInput{}, ctx.Err()
	}
	var stopDone <-chan sugahooks.Result
	if waiting != nil {
		stopDone = waiting.Done()
	}
	interrupted := false
	for {
		// Prefer a byte already observed by the reader over a simultaneous Stop result.
		select {
		case event := <-source.Next():
			in, again, err := processChatInputEvent(source, event, waiting, lost)
			if again {
				interrupted = true
				waiting = nil
				stopDone = nil
				continue
			}
			in.interrupted = interrupted
			return in, err
		case <-source.taskWake:
			// A background task ended while chat waits: its notice starts a turn.
			text := source.taskNotice()
			if text == "" {
				continue
			}
			if waiting != nil {
				*lost = appendContext(*lost, waiting.Interrupt(context.Background()))
				interrupted = true
			}
			render.External(text)
			return chatInput{line: text, hasLine: true, injected: true, interrupted: interrupted}, nil
		case delivery := <-source.deliveries:
			if waiting != nil {
				*lost = appendContext(*lost, waiting.Interrupt(context.Background()))
				interrupted = true
			}
			render.External(delivery.line)
			delivery.settle(nil)
			return chatInput{line: delivery.line, hasLine: true, injected: true, interrupted: interrupted}, nil
		default:
		}
		select {
		case event := <-source.Next():
			in, again, err := processChatInputEvent(source, event, waiting, lost)
			if again {
				interrupted = true
				waiting = nil
				stopDone = nil
				continue
			}
			in.interrupted = interrupted
			return in, err
		case result, ok := <-stopDone:
			if ok && result.Response.Notice != "" {
				fmt.Fprintln(source, result.Response.Notice)
			}
			if !ok || result.Err != nil || result.Response.Decision != "block" || result.Response.Reason == "" {
				fmt.Fprintln(source)
				return chatInput{stopDone: true, interrupted: interrupted}, nil
			}
			render.External(result.Response.Reason)
			return chatInput{line: result.Response.Reason, hasLine: true, stopDone: true, injected: true, interrupted: interrupted}, nil
		case <-source.taskWake:
			// A background task ended while chat waits: its notice starts a turn.
			text := source.taskNotice()
			if text == "" {
				continue
			}
			if waiting != nil {
				*lost = appendContext(*lost, waiting.Interrupt(context.Background()))
				interrupted = true
			}
			render.External(text)
			return chatInput{line: text, hasLine: true, injected: true, interrupted: interrupted}, nil
		case delivery := <-source.deliveries:
			if waiting != nil {
				*lost = appendContext(*lost, waiting.Interrupt(context.Background()))
				interrupted = true
			}
			render.External(delivery.line)
			delivery.settle(nil)
			return chatInput{line: delivery.line, hasLine: true, injected: delivery.injected, interrupted: interrupted}, nil
		case <-ctx.Done():
			return chatInput{interrupted: interrupted}, ctx.Err()
		}
	}
}

// processChatInputEvent turns one reader event into input. again reports an
// event that only cut a Stop wait short (typing, or Ctrl-C/EOF while waiting):
// the caller keeps reading instead of returning it.
func processChatInputEvent(source *chatInputSource, event chatInputEvent, waiting sugahooks.Wait, lost *string) (in chatInput, again bool, err error) {
	if event.activity {
		if waiting != nil {
			*lost = appendContext(*lost, waiting.Interrupt(context.Background()))
		}
		source.AckActivity()
		return chatInput{}, true, nil
	}
	if event.err != nil && waiting != nil {
		*lost = appendContext(*lost, waiting.Interrupt(context.Background()))
		return chatInput{line: event.line}, true, event.err
	}
	source.CompleteLine()
	return chatInput{line: event.line, hasLine: event.err == nil}, false, event.err
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
func showHookNotice(w io.Writer, response sugahooks.Response) {
	if response.Notice != "" {
		fmt.Fprintln(w, response.Notice)
	}
}

// exitConfirmWindow is how soon a second Ctrl-C at the prompt must follow the
// first to exit chat.
const exitConfirmWindow = 3 * time.Second

// chatStats accumulates what one chat process used, for the exit summary.
type chatStats struct {
	turns   int
	usage   agentcore.Usage
	session agentcore.Usage // session-cumulative usage after the last turn
}

func (s *chatStats) add(result personal.RunResult) {
	s.turns++
	s.usage = s.usage.Add(result.Usage)
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
	// Leading newline: Ctrl-C/Ctrl-D at the prompt leaves the cursor after "❯ ".
	fmt.Fprintf(w, "\n%s · 运行 %s\n续接：suga chat --resume %s\n", line, formatElapsed(elapsed), sessionID)
}
