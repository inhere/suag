package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/cflag"
	"github.com/gookit/goutil/cflag/capp"
	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/internal/platform/tools/native"
	"github.com/inhere/suag/pkg/agentcore"
)

var ErrInputRequired = errors.New("turn requires more input")

type personalOptions struct {
	Prompt    string
	Model     string
	Provider  string
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
	// --append-system-prompt keeps it instead: Gofer templates always pass the
	// flag and render it empty when a job has no system prompt.
	ClearAppendSystemPrompt bool
	NoProjectRules          bool
	MCPConfigs              []string
	CLIMode                 bool
}

type personalRuntime struct {
	runner       *personal.Runner
	repo         *store.Store
	capabilities defaultCapabilities
	gate         *cliMCPGate
	// contextWindow is the window of the model actually used by the provider.
	contextWindow int
}

func (p *personalRuntime) Close() error {
	if p == nil {
		return nil
	}
	return errors.Join(p.repo.Close(), p.capabilities.Close())
}

// RunPersonalCLI is the shared headless and interactive entry, independent of the old Web host.
func RunPersonalCLI(ctx context.Context, mode string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if mode != "run" && mode != "chat" {
		return fmt.Errorf("unsupported personal mode %q", mode)
	}
	return NewPersonalCommand(ctx, mode, stdin, stdout, stderr).Parse(args)
}

func runPersonalWithOptions(ctx context.Context, mode string, opts personalOptions, stdin io.Reader, stdout, stderr io.Writer) (retErr error) {
	runtime, err := openPersonalRuntime(ctx, opts.AllowWrite && !opts.ReadOnly, opts)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, runtime.Close()) }()
	if mode == "chat" {
		return runPersonalChat(ctx, runtime.runner, opts, stdin, stdout, stderr, runtime.gate)
	}
	return executePersonalRun(ctx, runtime.runner, opts, stdout, stderr, runtime.gate)
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
	cmd.StringVar(&opts.Model, "model", "", "model name (overrides SUAG_MODEL)")
	cmd.StringVar(&opts.Provider, "provider", "", "provider name (overrides SUAG_PROVIDER)")
	cmd.StringVar(&opts.SessionID, "session-id", "", "create or use this session ID;;sid")
	cmd.StringVar(&opts.ResumeID, "resume", "", "resume an existing session ID;;r")
	cmd.StringVar(&opts.FromID, "from", "", "start a new session that inherits the summary, plan and loaded tools of this session")
	cmd.StringVar(&opts.OutputFormat, "output-format", "text", "text or stream-json")
	cmd.BoolVar(&opts.ReadOnly, "read-only", false, "disable every mutating tool; MCP allows only default query tools and gofer_work_report;;ro")
	cmd.BoolVar(&opts.AllowWrite, "allow-write", false, "allow workspace write and shell tools for this run")
	cmd.StringVar(&opts.AppendSystemPrompt, "append-system-prompt", "", "append system instructions after rules; empty keeps the saved prompt")
	cmd.BoolVar(&opts.ClearAppendSystemPrompt, "clear-append-system-prompt", false, "clear the session's saved append prompt (a non-empty --append-system-prompt still replaces it)")
	cmd.BoolVar(&opts.NoProjectRules, "no-project-rules", false, "skip project rule files for debugging")
	cmd.Var((*cflag.Strings)(&opts.MCPConfigs), "mcp-config", "Claude-style JSON MCP server config; repeatable")
	return cmd
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

func openPersonalRuntime(ctx context.Context, allowWrite bool, opts personalOptions) (*personalRuntime, error) {
	workspace, err := filepath.Abs(appconfig.ResolveWorkspacePath())
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	servers, err := appconfig.ReadClaudeMCPConfigs(opts.MCPConfigs)
	if err != nil {
		return nil, err
	}
	opts.CLIMode = true
	return openPersonalRuntimeConfigured(ctx, workspace, cwd, allowWrite, servers, nil, opts)
}

func openPersonalRuntimeConfigured(ctx context.Context, workspace, ruleCWD string, allowWrite bool, servers []appconfig.MCPServerSettings, hooks []agentcore.Hook, opts personalOptions) (*personalRuntime, error) {
	settings := appconfig.ReadLLMProviderSettingsWithOverrides(opts.Model, opts.Provider)
	provider, err := appconfig.BuildLLMProviderFromSettings(settings)
	if err != nil {
		return nil, err
	}
	var capabilities defaultCapabilities
	if opts.CLIMode {
		capabilities, err = buildPersonalCLICapabilities(ctx, workspace, allowWrite, servers)
	} else {
		capabilities, err = buildPersonalCapabilitiesWithServers(ctx, workspace, allowWrite, servers)
	}
	if err != nil {
		return nil, err
	}
	// update_plan only writes session state, so it is registered in every mode
	// (including read-only). The recorder is bound to the runner in personal.New.
	plans := personal.NewPlanRecorder()
	capabilities.Registry.Register(native.NewUpdatePlanTool(plans))
	var gate *cliMCPGate
	var toolRegistry agentcore.ToolRegistry = capabilities.Registry
	if opts.CLIMode {
		gate, err = newCLIMCPGate(capabilities.Registry, opts.ReadOnly)
		if err != nil {
			_ = capabilities.Close()
			return nil, err
		}
		toolRegistry = gate
		hooks = append(hooks, gate)
	}
	// Visibility sits between observe (added in personal.New) and the approval
	// layer: observe -> visibility -> approval(gate) -> base registry.
	vis, err := newToolVisibility(capabilities.Registry, toolRegistry, contextWindowForSettings(settings))
	if err != nil {
		_ = capabilities.Close()
		return nil, err
	}
	toolRegistry = vis
	dbPath, err := appconfig.ResolveCoreStorePath()
	if err != nil {
		_ = capabilities.Close()
		return nil, fmt.Errorf("resolve personal session database: %w", err)
	}
	transcriptDir := ""
	if transcriptsEnabled() {
		home, err := appconfig.ResolveSuagHome()
		if err != nil {
			_ = capabilities.Close()
			return nil, fmt.Errorf("resolve transcript root: %w", err)
		}
		transcriptDir = filepath.Join(home, "transcripts")
	}
	home, err := appconfig.ResolveSuagHome()
	if err != nil {
		_ = capabilities.Close()
		return nil, fmt.Errorf("resolve session process root: %w", err)
	}
	lockRoot := filepath.Join(home, "run", "locks")
	processRoot := filepath.Join(home, "run")
	repo, err := store.Open(dbPath)
	if err != nil {
		_ = capabilities.Close()
		return nil, err
	}
	// llm.compact_model: summaries use that model on the same provider; unset
	// means the turn's own provider.
	var compactProvider agentcore.Provider
	compactSettings, hasCompact := appconfig.CompactModelSettings(settings)
	if hasCompact {
		built, err := appconfig.BuildLLMProviderFromSettings(compactSettings)
		if err != nil {
			_ = repo.Close()
			_ = capabilities.Close()
			return nil, fmt.Errorf("build compact model provider: %w", err)
		}
		compactProvider = built
	}
	runner, err := personal.New(personal.Config{
		ContextWindow:   contextWindowForSettings(settings),
		CompactProvider: compactProvider,
		CompactModel:    compactSettings.Model,
		Plans:           plans,
		Visibility:      vis,
		Repository:      repo,
		Provider:        provider,
		Model:           settings.Model,
		ProviderName:    settings.ProviderName,
		Tools:           toolRegistry,
		Hooks:           hooks,
		MaxSteps:        gkconfig.Int("max_steps", 30),
		RuleSource: func() (string, string, error) {
			home, err := appconfig.ResolveSuagHome()
			if err != nil {
				return "", "", err
			}
			return personal.LoadRules(ruleCWD, home, opts.NoProjectRules)
		},
		AppendSystemPrompt:    appendSystemPrompt(opts.AppendSystemPrompt),
		AppendSystemPromptSet: opts.ClearAppendSystemPrompt,
		GoferJob:              os.Getenv("GOFER_JOB_ID") != "",
		TranscriptDir:         transcriptDir,
		LockRoot:              lockRoot,
		ProcessRoot:           processRoot,
	})
	if err != nil {
		_ = repo.Close()
		_ = capabilities.Close()
		return nil, err
	}
	// Same model value the provider was built from (flag/env overrides included).
	return &personalRuntime{runner: runner, repo: repo, capabilities: capabilities, gate: gate,
		contextWindow: contextWindowForSettings(settings)}, nil
}

// contextWindowForSettings resolves the usage_update size from the effective
// model: llm.context_window wins, then the model-name table, then the default.
func contextWindowForSettings(settings appconfig.LLMProviderSettings) int {
	if configured := gkconfig.Int("llm.context_window"); configured > 0 {
		return configured
	}
	return appconfig.ContextWindowForModel(settings.Model)
}

func appendSystemPrompt(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv("SUAG_APPEND_SYSTEM_PROMPT")
}

func buildPersonalCapabilities(workspace string, allowWrite bool) (defaultCapabilities, error) {
	return buildPersonalCapabilitiesWithServers(context.Background(), workspace, allowWrite, nil)
}

func buildPersonalCapabilitiesWithServers(ctx context.Context, workspace string, allowWrite bool, servers []appconfig.MCPServerSettings) (defaultCapabilities, error) {
	if allowWrite {
		return buildDefaultCapabilitiesWithServers(ctx, workspace, servers)
	}
	registry := tools.NewRegistry()
	for _, tool := range native.NewFileTools(workspace) {
		if tool.Name() == "file_read" || tool.Name() == "file_list" {
			registry.Register(tool)
		}
	}
	return defaultCapabilities{Registry: registry}, nil
}

// CLI always exposes configured MCP tools; the dispatch gate enforces their permissions.
func buildPersonalCLICapabilities(ctx context.Context, workspace string, allowWrite bool, servers []appconfig.MCPServerSettings) (defaultCapabilities, error) {
	if allowWrite {
		return buildDefaultCapabilitiesWithServers(ctx, workspace, servers)
	}
	built, err := buildPersonalCapabilitiesWithServers(ctx, workspace, false, nil)
	if err != nil {
		return defaultCapabilities{}, err
	}
	for _, settings := range append(appconfig.ReadMCPServerSettings(), servers...) {
		provider, err := mcpProviderFactory(ctx, settings)
		if err != nil {
			_ = built.Close()
			return defaultCapabilities{}, fmt.Errorf("build mcp provider %q: %w", settings.ID, err)
		}
		if err := provider.RegisterTools(built.Registry); err != nil {
			_ = provider.Close()
			_ = built.Close()
			return defaultCapabilities{}, fmt.Errorf("register mcp tools for %q: %w", settings.ID, err)
		}
		built.closers = append(built.closers, provider)
	}
	return built, nil
}

func executePersonalRun(ctx context.Context, runner *personal.Runner, opts personalOptions, stdout, stderr io.Writer, gates ...*cliMCPGate) error {
	var gate *cliMCPGate
	if len(gates) > 0 {
		gate = gates[0]
	}
	if gate != nil {
		gate.Reset()
	}
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
		case "plan":
			// Same shape as the ACP plan update: the full list, replaced each time.
			entries := make([]map[string]string, 0, len(event.Plan.Steps))
			for _, step := range event.Plan.Steps {
				entries = append(entries, map[string]string{"step": step.Step, "status": step.Status})
			}
			data["type"], data["explanation"], data["entries"] = "plan", event.Plan.Explanation, entries
		default:
			return nil
		}
		return encoder.Encode(data)
	}
	result, err := runner.Run(ctx, personal.RunRequest{SessionID: sessionID, Prompt: opts.Prompt, Emit: emit})
	if err != nil {
		if encoder != nil {
			_ = encoder.Encode(withUsage(map[string]any{"type": "error", "session_id": sessionID, "error": err.Error()}, result))
		}
		return err
	}
	if result.Status == agentcore.RunYielded {
		if encoder != nil {
			_ = encoder.Encode(withUsage(map[string]any{"type": "input_required", "session_id": sessionID, "run_id": result.RunID}, result))
		}
		return ErrInputRequired
	}
	if gate != nil && gate.WasDenied() && encoder != nil {
		if err := encoder.Encode(withUsage(map[string]any{"type": "input_required", "session_id": sessionID, "run_id": result.RunID}, result)); err != nil {
			return err
		}
	}
	if encoder != nil {
		return encoder.Encode(withUsage(map[string]any{
			"type": "result", "session_id": sessionID, "run_id": result.RunID,
			"result": result.Output, "status": result.State,
		}, result))
	}
	_, err = fmt.Fprintln(stdout, result.Output)
	return err
}

// withUsage adds usage to a stream-json line: "usage" is this turn, and
// "session_usage" the session-cumulative total. Both use the reported shape
// (input_tokens excludes cache; see personal.ReportUsage), identical on the
// result, input_required and error lines.
func withUsage(line map[string]any, result personal.RunResult) map[string]any {
	line["usage"] = personal.ReportUsage(result.Usage)
	line["session_usage"] = personal.ReportUsage(result.SessionUsage)
	return line
}

func openRequestedSession(ctx context.Context, runner *personal.Runner, opts personalOptions) (string, error) {
	if opts.FromID != "" {
		// --session-id names the new session; an existing one is rejected by Handoff.
		model, err := runner.Handoff(ctx, personal.HandoffRequest{SourceID: opts.FromID, NewID: opts.SessionID, Title: opts.Prompt})
		if err != nil {
			return "", err
		}
		return model.ID, nil
	}
	if opts.ResumeID != "" {
		model, err := runner.LoadSession(ctx, opts.ResumeID)
		if err != nil {
			return "", err
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

// RunPersonalChat gives the terminal REPL the same persistent session engine as run mode.
func RunPersonalChat(ctx context.Context, args []string) error {
	return RunPersonalCLI(ctx, "chat", args, os.Stdin, os.Stdout, os.Stderr)
}
