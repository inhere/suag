package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	gkconfig "github.com/gookit/config/v2"
	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/host/session"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/internal/platform/tools/native"
	"github.com/inhere/suag/pkg/agentcore"
)

var ErrInputRequired = errors.New("turn requires more input")

type personalOptions struct {
	Prompt       string
	SessionID    string
	ResumeID     string
	OutputFormat string
	ReadOnly     bool
	AllowWrite   bool
}

type personalRuntime struct {
	runner       *personal.Runner
	repo         *session.SQLiteRepository
	capabilities defaultCapabilities
}

func (p *personalRuntime) Close() error {
	if p == nil {
		return nil
	}
	return errors.Join(p.repo.Close(), p.capabilities.Close())
}

// RunPersonalCLI is the shared headless and interactive entry, independent of the old Web host.
func RunPersonalCLI(ctx context.Context, mode string, args []string, stdin io.Reader, stdout, stderr io.Writer) (retErr error) {
	opts, err := parsePersonalOptions(mode, args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	runtime, err := openPersonalRuntime(ctx, opts.AllowWrite && !opts.ReadOnly)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, runtime.Close()) }()
	if mode == "chat" {
		return runPersonalChat(ctx, runtime.runner, opts, stdin, stdout, stderr)
	}
	return executePersonalRun(ctx, runtime.runner, opts, stdout, stderr)
}

func parsePersonalOptions(mode string, args []string, stderr io.Writer) (personalOptions, error) {
	if mode != "run" && mode != "chat" {
		return personalOptions{}, fmt.Errorf("unsupported personal mode %q", mode)
	}
	var opts personalOptions
	fs := flag.NewFlagSet(mode, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&opts.Prompt, "prompt", "", "task prompt for run mode")
	fs.StringVar(&opts.Prompt, "p", "", "task prompt for run mode")
	fs.StringVar(&opts.SessionID, "session-id", "", "create or use this session ID")
	fs.StringVar(&opts.ResumeID, "resume", "", "resume an existing session ID")
	fs.StringVar(&opts.OutputFormat, "output-format", "text", "text or stream-json")
	fs.BoolVar(&opts.ReadOnly, "read-only", false, "disable every mutating tool")
	fs.BoolVar(&opts.AllowWrite, "allow-write", false, "allow workspace write and shell tools for this run")
	if err := fs.Parse(args); err != nil {
		return personalOptions{}, err
	}
	if fs.NArg() != 0 {
		return personalOptions{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	if opts.SessionID != "" && opts.ResumeID != "" {
		return personalOptions{}, fmt.Errorf("--session-id and --resume cannot be combined")
	}
	if opts.OutputFormat != "text" && opts.OutputFormat != "stream-json" {
		return personalOptions{}, fmt.Errorf("unsupported output format %q", opts.OutputFormat)
	}
	if mode == "run" && strings.TrimSpace(opts.Prompt) == "" {
		return personalOptions{}, fmt.Errorf("--prompt is required for run mode")
	}
	if mode == "chat" && opts.Prompt != "" {
		return personalOptions{}, fmt.Errorf("--prompt is only supported in run mode")
	}
	if mode == "chat" && opts.OutputFormat != "text" {
		return personalOptions{}, fmt.Errorf("chat mode requires text output")
	}
	return opts, nil
}

func openPersonalRuntime(ctx context.Context, allowWrite bool) (*personalRuntime, error) {
	provider, err := appconfig.BuildLLMProvider()
	if err != nil {
		return nil, err
	}
	workspace, err := filepath.Abs(appconfig.ResolveWorkspacePath())
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	capabilities, err := buildPersonalCapabilities(workspace, allowWrite)
	if err != nil {
		return nil, err
	}
	dbPath, err := appconfig.ResolvePersonalSessionDBPath()
	if err != nil {
		_ = capabilities.Close()
		return nil, fmt.Errorf("resolve personal session database: %w", err)
	}
	runPath, err := appconfig.ResolvePersonalAgentcoreRunRoot()
	if err != nil {
		_ = capabilities.Close()
		return nil, fmt.Errorf("resolve personal run root: %w", err)
	}
	runRoot, err := filepath.Abs(runPath)
	if err != nil {
		_ = capabilities.Close()
		return nil, fmt.Errorf("resolve run root: %w", err)
	}
	if err := prepareHostSessionDatabase(ctx, dbPath); err != nil {
		_ = capabilities.Close()
		return nil, err
	}
	repo, err := session.NewSQLiteRepository(dbPath)
	if err != nil {
		_ = capabilities.Close()
		return nil, err
	}
	runner, err := personal.New(personal.Config{
		Repository: repo,
		RunRoot:    runRoot,
		Provider:   provider,
		Tools:      capabilities.Registry,
		MaxSteps:   gkconfig.Int("max_steps", 30),
	})
	if err != nil {
		_ = repo.Close()
		_ = capabilities.Close()
		return nil, err
	}
	return &personalRuntime{runner: runner, repo: repo, capabilities: capabilities}, nil
}

func buildPersonalCapabilities(workspace string, allowWrite bool) (defaultCapabilities, error) {
	if allowWrite {
		return buildDefaultCapabilities(workspace)
	}
	registry := tools.NewRegistry()
	for _, tool := range native.NewFileTools(workspace) {
		if tool.Name() == "file_read" || tool.Name() == "file_list" {
			registry.Register(tool)
		}
	}
	return defaultCapabilities{Registry: registry}, nil
}

func executePersonalRun(ctx context.Context, runner *personal.Runner, opts personalOptions, stdout, stderr io.Writer) error {
	sessionID, err := openRequestedSession(ctx, runner, opts)
	if err != nil {
		return err
	}
	var encoder *json.Encoder
	if opts.OutputFormat == "stream-json" {
		encoder = json.NewEncoder(stdout)
		if err := encoder.Encode(map[string]any{"type": "session", "id": sessionID, "session_id": sessionID}); err != nil {
			return err
		}
	} else {
		fmt.Fprintln(stderr, "session id:", sessionID)
	}
	emit := func(_ context.Context, event personal.Event) error {
		if encoder == nil {
			return nil
		}
		data := map[string]any{"session_id": event.SessionID, "run_id": event.RunID}
		switch event.Kind {
		case "run_started":
			data["type"] = "turn_start"
		case "run_finished":
			data["type"], data["status"] = "turn_end", event.State
		case string(agentcore.StreamEventTextDelta):
			data["type"], data["text"] = "message_delta", event.Text
		default:
			return nil
		}
		return encoder.Encode(data)
	}
	result, err := runner.Run(ctx, personal.RunRequest{SessionID: sessionID, Prompt: opts.Prompt, Emit: emit})
	if err != nil {
		if encoder != nil {
			_ = encoder.Encode(map[string]any{"type": "error", "session_id": sessionID, "error": err.Error()})
		}
		return err
	}
	if result.Status == agentcore.RunYielded {
		if encoder != nil {
			_ = encoder.Encode(map[string]any{"type": "input_required", "session_id": sessionID, "run_id": result.RunID})
		}
		return ErrInputRequired
	}
	if encoder != nil {
		return encoder.Encode(map[string]any{
			"type": "result", "session_id": sessionID, "run_id": result.RunID,
			"result": result.Output, "status": result.State, "usage": result.Usage,
		})
	}
	_, err = fmt.Fprintln(stdout, result.Output)
	return err
}

func openRequestedSession(ctx context.Context, runner *personal.Runner, opts personalOptions) (string, error) {
	if opts.ResumeID != "" {
		model, err := runner.LoadSession(ctx, opts.ResumeID)
		if err != nil {
			return "", err
		}
		return model.SessionID, nil
	}
	if opts.SessionID != "" {
		model, err := runner.LoadSession(ctx, opts.SessionID)
		if err == nil {
			return model.SessionID, nil
		}
		if !errors.Is(err, session.ErrSessionNotFound) {
			return "", err
		}
		model, err = runner.NewSession(ctx, opts.SessionID, opts.Prompt)
		if err != nil {
			return "", err
		}
		return model.SessionID, nil
	}
	model, err := runner.NewSession(ctx, "", opts.Prompt)
	if err != nil {
		return "", err
	}
	return model.SessionID, nil
}

// RunPersonalChat gives the terminal REPL the same persistent session engine as run mode.
func RunPersonalChat(ctx context.Context, args []string) error {
	return RunPersonalCLI(ctx, "chat", args, os.Stdin, os.Stdout, os.Stderr)
}
