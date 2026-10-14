package chat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	sugahooks "github.com/inhere/suga/internal/hooks"
	"github.com/inhere/suga/internal/personal"
	personalprocess "github.com/inhere/suga/internal/personal/process"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/redact"
	"github.com/inhere/suga/pkg/agentcore/tools"
)

// chatState is what one chat process carries besides the session setup Run
// does: the wiring a turn needs and the state that lives across turns. One turn
// runs as runTurn on it, so turn logic is extended here rather than through
// another parameter of Run.
type chatState struct {
	// Wiring made once by Run.
	ctx         context.Context
	runner      *personal.Runner
	hooks       sugahooks.Client
	source      *chatInputSource
	render      *Renderer
	stderr      io.Writer
	baseEvent   func(name string) sugahooks.Event
	title       *terminalTitle
	idle        *idleConsolidation
	server      *personalprocess.Server
	bgStop      *backgroundStop
	stats       *chatStats
	sessionID   string
	sessionName string

	// waiting is the Stop hook wait of the turn that just ran: runTurn starts
	// it, the next iteration of Run's loop consumes it.
	waiting sugahooks.Wait
	// lostReply holds a web reply that arrived while the user interrupted the Stop wait.
	// It is folded into the next turn's context; if the chat ends first, warn on stderr.
	lostReply string
	// stopHookActive is set while a Stop hook asked to continue: the next turn
	// runs as its follow-up.
	stopHookActive bool
	// pendingContext is the extra context collected for the next prompt (hook
	// AdditionalContext, a setup context, a lost reply).
	pendingContext string
	// createdAtStartup is true while the session has no title yet: the next
	// message names it.
	createdAtStartup bool
}

// runTurn executes one turn of the chat: from pausing the idle consolidation to
// the Stop hook of the finished turn being started. It returns the error that
// ends the chat (an interrupted turn, or a failure the chat cannot continue
// from); a turn that merely failed, yielded or answered reports itself through
// the renderer and the stats.
func (s *chatState) runTurn(line string, injected bool) error {
	// A turn starts: no idle consolidation until it has ended (EndTurn).
	s.idle.Pause()
	if s.createdAtStartup {
		if err := s.runner.SetSessionTitle(s.ctx, s.sessionID, line); err != nil {
			return err
		}
		// The first message names the session: the terminal title follows the
		// stored title (same derivation, so both read the same).
		s.title.Set(s.sessionName, personal.SessionTitle(line))
		s.createdAtStartup = false
	}
	if s.hooks != nil {
		submit := s.baseEvent("UserPromptSubmit")
		submit.Prompt = redact.Secrets(line)
		// injected covers every non-keyboard source (Stop block reasons and socket
		// deliveries both flow through nextChatInput), same flag the transcript records.
		submit.Injected = injected
		submitResp := s.hooks.Call(s.ctx, submit)
		showHookNotice(s.stderr, submitResp)
		s.pendingContext = appendContext(s.pendingContext, sugahooks.AdditionalContext(submitResp))
	}
	if s.lostReply != "" {
		// A reply received during an interrupt race still reaches the model as next-turn context.
		fmt.Fprintln(s.stderr, "Note: delivering hook reply received while interrupting:", sugahooks.TruncateOutput(s.lostReply, 200))
		s.pendingContext = appendContext(s.pendingContext, s.lostReply)
		s.lostReply = ""
	}
	// A background Stop that asked to continue makes this turn its follow-up.
	turnStopActive := s.stopHookActive || s.bgStop.takeBlocked()
	s.stopHookActive = false
	s.source.SetBusy(true)
	if s.server != nil {
		_ = s.server.SetBusy(true)
	}
	s.render.BeginTurn()
	turnStart := time.Now()
	result, runErr := s.runner.Run(s.ctx, personal.RunRequest{
		SessionID:     s.sessionID,
		Prompt:        line,
		SystemContext: s.pendingContext,
		Injected:      injected,
		Emit: func(eventCtx context.Context, event personal.Event) error {
			s.render.Handle(event)
			if s.hooks == nil || event.Kind != "tool_result" {
				return nil
			}
			// hooks.post_tool_use decides which tools send PostToolUse and under which name.
			toolName, send := s.hooks.PostToolName(event.ToolName)
			if !send {
				return nil
			}
			postTool := s.baseEvent("PostToolUse")
			postTool.ToolName = toolName
			postTool.ToolOutput = sugahooks.TruncateOutput(redact.Secrets(event.Text), toolHookLimit)
			// The notice goes to stderr while the waiting line sits on stdout: take
			// that line down (the tool result above started a new one) so the two
			// never end up on the same screen line, and put it back afterwards.
			s.render.stopThinking()
			showHookNotice(s.stderr, s.hooks.Call(eventCtx, postTool))
			s.render.startThinking()
			return nil
		},
	})
	s.source.SetBusy(false)
	if s.server != nil {
		_ = s.server.SetBusy(false)
	}
	s.stats.add(result)
	s.pendingContext = ""
	// Every turn end (success, error or yield) must send Stop: the hook contract defines it as
	// "turn finished, back to waiting for input", otherwise a hook-side session stays "running" forever.
	stopMessage := ""
	if runErr != nil {
		s.render.Finish()
		switch {
		case s.ctx.Err() != nil:
			// Ctrl-C (SIGINT) cut this turn short: chat ends here as before
			// (cmd/suga reports 已中断 with exit 130), and the turn itself is
			// reported as interrupted rather than with the runner's raw
			// "context canceled".
			s.render.Interrupt()
			return s.ctx.Err()
		case errors.Is(runErr, tools.ErrAborted):
			// Ctrl-C/Ctrl-D at an approval question: only this turn ends.
			s.render.Notice("已取消")
		case errors.Is(runErr, context.Canceled):
			// A cancelled turn that did not cancel this chat itself (a
			// cancelled model call): the raw error would read
			// "context canceled".
			s.render.Interrupt()
		default:
			s.render.Error(runErr)
		}
		stopMessage = "Error: " + runErr.Error()
	} else if result.Status == agentcore.RunYielded {
		s.render.Finish()
		s.render.Notice("Waiting for more input.")
		s.render.Status(result, time.Since(turnStart))
	} else {
		// Streamed text is already on screen; otherwise print the answer now.
		if !s.render.Finish() {
			s.render.Answer(result.Output)
		}
		if len(result.DeniedTools) > 0 {
			s.render.Notice("本轮被拒绝的工具：" + strings.Join(result.DeniedTools, ", "))
		}
		s.render.Status(result, time.Since(turnStart))
		stopMessage = result.Output
	}
	if s.hooks != nil {
		stop := s.baseEvent("Stop")
		stop.LastAssistant = sugahooks.TruncateOutput(redact.Secrets(stopMessage), stopHookLimit)
		stop.StopHookActive = turnStopActive
		if s.bgStop == nil {
			s.waiting = s.hooks.StartStop(s.ctx, stop)
		} else if err := s.bgStop.start(s.ctx, stop); err != nil {
			// No task slot (tasks.max_running): wait at the prompt as before.
			s.render.Notice("Stop hook 未能在后台运行，改为等待：" + err.Error())
			s.waiting = s.hooks.StartStop(s.ctx, stop)
		}
	}
	// The turn is over (Stop sent): the idle clock runs from now.
	s.idle.EndTurn(result)
	return nil
}
