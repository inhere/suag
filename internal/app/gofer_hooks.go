package app

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	gkconfig "github.com/gookit/config/v2"
)

type goferHookEvent struct {
	Name           string `json:"hook_event_name"`
	SessionID      string `json:"session_id"`
	CWD            string `json:"cwd"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	Source         string `json:"source,omitempty"`
	Prompt         string `json:"prompt,omitempty"`
	// Injected marks a UserPromptSubmit whose input did not come from terminal typing
	// (Stop-block reply/notification or a local-socket delivery). Omitted when false.
	Injected       bool   `json:"injected,omitempty"`
	LastAssistant  string `json:"last_assistant_message,omitempty"`
	StopHookActive bool   `json:"stop_hook_active"`
	ToolName       string `json:"tool_name,omitempty"`
	ToolOutput     string `json:"tool_output,omitempty"`
}

type goferHookResponse struct {
	Decision          string
	Reason            string
	AdditionalContext string
	// Notice is a one-line message the Gofer server wants the person at this terminal
	// to see (e.g. the session was taken over on the web). `gofer hook` prints it on
	// its stderr, so the client captures that stream.
	Notice string
}

type goferHookResult struct {
	response goferHookResponse
	err      error
}

type chatHookWait interface {
	Done() <-chan goferHookResult
	// Interrupt ends the wait. It returns a web reply that had already been received
	// (Stop block) but not yet consumed, so the caller can still deliver it.
	Interrupt(context.Context) string
}

type chatHookClient interface {
	Call(context.Context, goferHookEvent) goferHookResponse
	StartStop(context.Context, goferHookEvent) chatHookWait
}

type goferHooks struct {
	command string
	agent   string
	timeout time.Duration
	waitSec int
}

func newGoferHooks() chatHookClient {
	if strings.TrimSpace(os.Getenv("GOFER_JOB_ID")) != "" {
		return nil
	}
	// SUAG_GOFER_HOOKS overrides gofer.hooks; tests set it to off so a local
	// `go test` never registers sessions with a live Gofer server.
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("SUAG_GOFER_HOOKS")))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(gkconfig.String("gofer.hooks")))
	}
	if mode == "" {
		mode = "auto"
	}
	if mode == "off" || (mode != "on" && mode != "auto") {
		return nil
	}
	command := strings.TrimSpace(gkconfig.String("gofer.command"))
	if command == "" {
		command = "gofer"
	}
	if mode == "auto" {
		if _, err := exec.LookPath(command); err != nil {
			return nil
		}
	}
	agent := strings.TrimSpace(gkconfig.String("gofer.agent"))
	if agent == "" {
		agent = "suag"
	}
	waitSec := gkconfig.Int("gofer.wait_sec", 7140)
	if waitSec <= 0 {
		waitSec = 7140
	}
	timeoutMS := gkconfig.Int("gofer.timeout_ms", 3000)
	if timeoutMS <= 0 {
		timeoutMS = 3000
	}
	return &goferHooks{command: command, agent: agent, waitSec: waitSec, timeout: time.Duration(timeoutMS) * time.Millisecond}
}

func (h *goferHooks) Call(ctx context.Context, event goferHookEvent) goferHookResponse {
	response, err := h.call(ctx, event, 0)
	if err != nil {
		slog.Warn("Gofer hook failed", "event", event.Name, "err", err)
	}
	return response
}

func (h *goferHooks) StartStop(ctx context.Context, event goferHookEvent) chatHookWait {
	waitCtx, cancel := context.WithCancel(ctx)
	done := make(chan goferHookResult, 1)
	wait := &goferStopWait{call: h.Call, event: event, cancel: cancel, done: done}
	go func() {
		response, err := h.call(waitCtx, event, h.waitSec)
		if err != nil {
			slog.Warn("Gofer hook failed", "event", event.Name, "err", err)
		}
		done <- goferHookResult{response: response, err: err}
		close(done)
	}()
	return wait
}

func (h *goferHooks) call(parent context.Context, event goferHookEvent, waitSec int) (goferHookResponse, error) {
	ctx := parent
	if waitSec == 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(parent, h.timeout)
		defer cancel()
	} else {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(parent, time.Duration(waitSec+2)*time.Second)
		defer cancel()
	}
	args := []string{"hook", "generic", "--agent", h.agent}
	if waitSec > 0 {
		args = append(args, "--wait", strconv.Itoa(waitSec))
	}
	input, err := json.Marshal(event)
	if err != nil {
		return goferHookResponse{}, err
	}
	input = append(input, '\n')
	cmd := exec.CommandContext(ctx, h.command, args...)
	cmd.Stdin = bytes.NewReader(input)
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return goferHookResponse{}, err
	}
	var wire struct {
		Decision           string `json:"decision"`
		Reason             string `json:"reason"`
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if len(bytes.TrimSpace(output)) != 0 {
		if err := json.Unmarshal(output, &wire); err != nil {
			return goferHookResponse{}, err
		}
	}
	return goferHookResponse{Decision: wire.Decision, Reason: wire.Reason, AdditionalContext: wire.HookSpecificOutput.AdditionalContext, Notice: strings.TrimSpace(stderr.String())}, nil
}

type goferStopWait struct {
	call   func(context.Context, goferHookEvent) goferHookResponse
	event  goferHookEvent
	cancel context.CancelFunc
	done   chan goferHookResult
}

func (w *goferStopWait) Done() <-chan goferHookResult { return w.done }

func (w *goferStopWait) Interrupt(parent context.Context) string {
	interrupt := w.event
	interrupt.Name = "Interrupt"
	interrupt.LastAssistant = ""
	interrupt.StopHookActive = false
	ctx, cancel := context.WithTimeout(parent, time.Second)
	w.call(ctx, interrupt)
	cancel()
	w.cancel()
	// The reply may have landed in done before cancellation; hand it back instead of dropping it.
	result := <-w.done
	if result.err == nil && result.response.Decision == "block" {
		return result.response.Reason
	}
	return ""
}

// limitedBuffer keeps at most noticeLimit bytes of a hook's stderr.
type limitedBuffer struct{ bytes.Buffer }

const noticeLimit = 1024

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := noticeLimit - b.Len(); room > 0 {
		b.Buffer.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string { return truncateHookOutput(b.Buffer.String(), noticeLimit) }

func hookAdditionalContext(response goferHookResponse) string {
	return strings.TrimSpace(response.AdditionalContext)
}

func truncateHookOutput(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) && len(value) > 0 {
		value = value[:len(value)-1]
	}
	return value
}
