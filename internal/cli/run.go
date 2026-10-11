package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gookit/goutil/cflag"
	"github.com/gookit/goutil/cflag/capp"
	"github.com/inhere/suag/internal/chat"
	"github.com/inhere/suag/internal/output"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/internal/wiring"
	"github.com/inhere/suag/pkg/agentcore"
)

var ErrInputRequired = errors.New("turn requires more input")

type personalOptions struct {
	Prompt    string
	Model     string
	SessionID string
	ResumeID  string
	// FromID starts a NEW session that inherits summary, plan and loaded tools
	// from this session (read-only on the source).
	FromID             string
	OutputFormat       string
	ReadOnly           bool
	AllowWrite         bool
	AppendSystemPrompt string
	// ClearAppendSystemPrompt drops the session's saved append prompt. An empty
	// --append-system-prompt keeps it instead: argv templates (job runners)
	// often always pass the flag and render it empty when there is no prompt.
	ClearAppendSystemPrompt bool
	NoProjectRules          bool
	// Injected (run only) marks this turn's prompt as not typed by a person;
	// the transcript records it as injected.
	Injected   bool
	MCPConfigs []string
	CLIMode    bool
}

// RunPersonalCLI is the shared headless and interactive entry, independent of the old Web host.
func RunPersonalCLI(ctx context.Context, mode string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if mode != "run" && mode != "chat" {
		return fmt.Errorf("unsupported personal mode %q", mode)
	}
	return NewPersonalCommand(ctx, mode, stdin, stdout, stderr).Parse(args)
}

func runPersonalWithOptions(ctx context.Context, mode string, opts personalOptions, stdin io.Reader, stdout, stderr io.Writer) (retErr error) {
	runtime, err := wiring.OpenCLI(ctx, opts.wiringOptions(mode))
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, runtime.Close()) }()
	if mode == "chat" {
		return runChat(ctx, runtime, opts, stdin, stdout, stderr)
	}
	return executePersonalRun(ctx, runtime.Runner, opts, stdout, stderr)
}

// NewPersonalCommand owns the flags and validation shared by direct CLI calls
// and the root command registry.
func NewPersonalCommand(ctx context.Context, mode string, stdin io.Reader, stdout, stderr io.Writer) *capp.Cmd {
	var opts personalOptions
	desc := "Run one personal-agent turn"
	if mode == "chat" {
		desc = "Chat with the personal agent"
	}
	cmd := capp.NewCmd(mode, desc, func(c *capp.Cmd) error {
		if len(c.RemainArgs()) != 0 {
			return fmt.Errorf("unexpected positional arguments: %s", strings.Join(c.RemainArgs(), " "))
		}
		if err := validatePersonalOptions(mode, opts); err != nil {
			return err
		}
		return runPersonalWithOptions(ctx, mode, opts, stdin, stdout, stderr)
	})
	cmd.SetOutput(stderr)
	cmd.StringVar(&opts.Prompt, "prompt", "", "task prompt for run mode;;p")
	cmd.StringVar(&opts.Model, "model", "", "model, <provider>/<model> or a bare model id (overrides SUAG_MODEL)")
	cmd.StringVar(&opts.SessionID, "session-id", "", "create or use this session ID;;sid")
	cmd.StringVar(&opts.ResumeID, "resume", "", "resume an existing session ID;;r")
	cmd.StringVar(&opts.FromID, "from", "", "start a new session that inherits the summary, plan and loaded tools of this session")
	cmd.StringVar(&opts.OutputFormat, "output-format", "text", "text or stream-json")
	cmd.BoolVar(&opts.ReadOnly, "read-only", false, "disable every mutating tool; MCP tools are limited to tools.read_only_allow{[;ro]}")
	cmd.BoolVar(&opts.AllowWrite, "allow-write", false, "allow workspace write and shell tools for this run")
	cmd.StringVar(&opts.AppendSystemPrompt, "append-system-prompt", "", "append system instructions after rules, empty keeps the saved prompt")
	cmd.BoolVar(&opts.ClearAppendSystemPrompt, "clear-append-system-prompt", false, "clear the session's saved append prompt (a non-empty --append-system-prompt still replaces it)")
	cmd.BoolVar(&opts.NoProjectRules, "no-project-rules", false, "skip project rule files for debugging")
	cmd.Var((*cflag.Strings)(&opts.MCPConfigs), "mcp-config", "Claude-style JSON MCP server config")
	if mode == "run" {
		cmd.BoolVar(&opts.Injected, "injected", false, "mark this turn's prompt as not typed by a person")
	}
	return cmd
}

// wiringOptions maps the CLI flags to the runtime assembly options. mode "chat"
// sets ChatMode (tools.allow_write and the chat approver apply only there).
func (o personalOptions) wiringOptions(mode string) wiring.Options {
	return wiring.Options{
		ChatMode:   mode == "chat",
		Model:      o.Model,
		AllowWrite: o.AllowWrite, ReadOnly: o.ReadOnly,
		// --read-only also limits the memory tool to get/search.
		MemoryReadOnly:     o.ReadOnly,
		MCPConfigs:         o.MCPConfigs,
		AppendSystemPrompt: o.AppendSystemPrompt, ClearAppendSystemPrompt: o.ClearAppendSystemPrompt,
		NoProjectRules: o.NoProjectRules,
	}
}

func validatePersonalOptions(mode string, opts personalOptions) error {
	if opts.SessionID != "" && opts.ResumeID != "" {
		return fmt.Errorf("--session-id and --resume cannot be combined")
	}
	if opts.FromID != "" && opts.ResumeID != "" {
		return fmt.Errorf("--from and --resume cannot be combined")
	}
	if opts.OutputFormat != "text" && opts.OutputFormat != "stream-json" {
		return fmt.Errorf("unsupported output format %q", opts.OutputFormat)
	}
	if mode == "run" && strings.TrimSpace(opts.Prompt) == "" {
		return fmt.Errorf("--prompt is required for run mode")
	}
	if mode == "chat" && opts.Prompt != "" {
		return fmt.Errorf("--prompt is only supported in run mode")
	}
	if mode == "chat" && opts.OutputFormat != "text" {
		return fmt.Errorf("chat mode requires text output")
	}
	return nil
}

func executePersonalRun(ctx context.Context, runner *personal.Runner, opts personalOptions, stdout, stderr io.Writer) error {
	sessionID, err := openRequestedSession(ctx, runner, opts)
	if err != nil {
		return err
	}
	var encoder *json.Encoder
	if opts.OutputFormat == "stream-json" {
		encoder = json.NewEncoder(stdout)
		if err := encoder.Encode(output.SessionLine(sessionID)); err != nil {
			return err
		}
	} else {
		fmt.Fprintln(stderr, "session id:", sessionID)
		fmt.Fprintln(stderr, "model:", chat.ModelLabel(runner.ProviderName(), runner.Model()))
	}
	live := output.NewLiveText(stdout)
	emit := func(_ context.Context, event personal.Event) error {
		if encoder == nil {
			live.Handle(event) // text mode prints the answer as it streams
			return nil
		}
		line, ok := output.EventLine(event)
		if !ok {
			return nil
		}
		return encoder.Encode(line)
	}
	runStart := time.Now()
	result, err := runner.Run(ctx, personal.RunRequest{SessionID: sessionID, Prompt: opts.Prompt, Injected: opts.Injected, Emit: emit})
	streamed := live.Finish() // end the streamed line before any error or status output
	if encoder == nil {
		// Text mode: a usage summary on stderr (stdout stays the answer only).
		fmt.Fprintln(stderr, runSummary(runner, result, time.Since(runStart)))
	}
	if err != nil {
		if encoder != nil {
			_ = encoder.Encode(output.ErrorLine(sessionID, err, result))
		}
		return err
	}
	if result.Status == agentcore.RunYielded {
		if encoder != nil {
			_ = encoder.Encode(output.YieldedLine(sessionID, result))
		}
		return ErrInputRequired
	}
	// A turn with any refused tool call ends with input_required, even when the
	// model worked around the refusal: the caller still learns authorization
	// was missing. denied_tools is the only field added to the line.
	if len(result.DeniedTools) > 0 && encoder != nil {
		if err := encoder.Encode(output.DeniedLine(sessionID, result)); err != nil {
			return err
		}
	}
	if encoder != nil {
		return encoder.Encode(output.ResultLine(sessionID, result))
	}
	if streamed {
		return nil
	}
	_, err = fmt.Fprintln(stdout, result.Output)
	return err
}

func openRequestedSession(ctx context.Context, runner *personal.Runner, opts personalOptions) (string, error) {
	if opts.FromID != "" {
		// --session-id names the new session; an existing one is rejected by Handoff.
		model, err := runner.Handoff(ctx, personal.HandoffRequest{SourceID: opts.FromID, NewID: opts.SessionID, Title: opts.Prompt})
		if err != nil {
			return "", personal.SessionNotFound(err, opts.FromID)
		}
		return model.ID, nil
	}
	if opts.ResumeID != "" {
		model, err := runner.LoadSession(ctx, opts.ResumeID)
		if err != nil {
			return "", personal.SessionNotFound(err, opts.ResumeID)
		}
		return model.ID, nil
	}
	if opts.SessionID != "" {
		model, err := runner.LoadSession(ctx, opts.SessionID)
		if err == nil {
			return model.ID, nil
		}
		if !errors.Is(err, personal.ErrSessionNotFound) {
			return "", err
		}
		model, err = runner.NewSession(ctx, opts.SessionID, opts.Prompt)
		if err != nil {
			return "", err
		}
		return model.ID, nil
	}
	model, err := runner.NewSession(ctx, "", opts.Prompt)
	if err != nil {
		return "", err
	}
	return model.ID, nil
}

// runSummary is the text-mode closing line: model, this turn's tokens, the
// session total when known, and the elapsed time.
func runSummary(runner *personal.Runner, result personal.RunResult, elapsed time.Duration) string {
	line := fmt.Sprintf("%s · in %d / out %d tokens", chat.ModelLabel(runner.ProviderName(), runner.Model()),
		result.Usage.InputTokens, result.Usage.OutputTokens)
	if s := result.SessionUsage; s.InputTokens > 0 || s.OutputTokens > 0 {
		line += fmt.Sprintf("（会话累计 in %d / out %d）", s.InputTokens, s.OutputTokens)
	}
	return line + " · " + chat.FormatElapsed(elapsed)
}
