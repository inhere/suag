// Package wiring assembles the personal runner (provider, tools, store, rules)
// shared by the CLI and ACP front ends.
package wiring

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gkconfig "github.com/gookit/config/v2"
	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/llm"
	"github.com/inhere/suag/pkg/agentcore/tools"
	"github.com/inhere/suag/pkg/agentcore/tools/mcp"
	"github.com/inhere/suag/pkg/agentcore/tools/native"
)

// Options carries everything needed to assemble one runtime.
type Options struct {
	// Workspace is the tool workspace root; RuleCWD is where project rules are read.
	// Both are filled by OpenCLI.
	Workspace string
	RuleCWD   string
	Model     string
	// AllowWrite exposes workspace write and shell tools.
	AllowWrite bool
	// ReadOnly (OpenCLI only) narrows the MCP gate to tools.read_only_allow and
	// overrides AllowWrite.
	ReadOnly bool
	// Servers are extra MCP servers (from --mcp-config or the ACP session).
	Servers []mcp.ServerSettings
	// MCPConfigs are Claude-style config files read by OpenCLI into Servers.
	MCPConfigs []string
	Hooks      []agentcore.Hook
	// Approver is the permission policy of the approval layer (run: MCP
	// allowlist; chat: chat approver set by OpenCLI; ACP: RequestPermission);
	// nil denies every tool call (fail closed).
	Approver           tools.Approver
	AppendSystemPrompt string
	// ClearAppendSystemPrompt drops the session's saved append prompt. An empty
	// AppendSystemPrompt keeps it instead: argv templates (job runners)
	// often always pass the flag and render it empty when there is no prompt.
	ClearAppendSystemPrompt bool
	NoProjectRules          bool
	// CLIMode always exposes configured MCP tools; the dispatch gate enforces permissions.
	CLIMode bool
	// ChatMode (OpenCLI only) marks the interactive chat: tools.allow_write may
	// enable write tools there, and tools are approved by the chat approver.
	ChatMode bool
	// MemoryReadOnly (--read-only, ACP read_only) limits the memory tool to
	// get/search. The zero value allows memory writes: memory is suag's own
	// state, not the workspace, so a plain run may write it.
	MemoryReadOnly bool
	// MemoryConfirm confirms memory writes of tainted sessions (ACP: the turn
	// gate's permission request; chat: set by OpenCLI to ask at the terminal).
	// nil (run) refuses them.
	MemoryConfirm MemoryConfirmFunc
}

// Runtime is one assembled personal runner and the resources it owns.
type Runtime struct {
	Runner *personal.Runner
	// Registry is the base tool registry (lets ACP tell MCP tools apart).
	Registry *tools.Registry
	// ContextWindow is the window of the model actually used by the provider.
	ContextWindow int
	// Asker (chat mode only, else nil) is where the chat approver asks its
	// questions; chat binds its terminal input to it. Unbound, every tool that
	// needs a confirmation is refused.
	Asker *tools.BindableAsker
	// WriteTools reports whether workspace write and shell tools are exposed.
	WriteTools bool

	repo         *store.Store
	capabilities defaultCapabilities
	// tools is the registry stack the runner calls (visibility -> approval
	// -> base); kept for tests.
	tools agentcore.ToolRegistry
}

// Close releases the session store and tool providers.
func (p *Runtime) Close() error {
	if p == nil {
		return nil
	}
	return errors.Join(p.repo.Close(), p.capabilities.Close())
}

// OpenCLI resolves the workspace, cwd, MCP configs and the CLI MCP gate from
// opts, then assembles the runtime via Open.
func OpenCLI(ctx context.Context, opts Options) (*Runtime, error) {
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
	// The run approver is the MCP allowlist (cliMCPGate); it never prompts.
	gate, err := newCLIMCPGate(opts.ReadOnly)
	if err != nil {
		return nil, err
	}
	var approver tools.Approver = gate
	var asker *tools.BindableAsker
	if opts.ChatMode {
		// chat wraps the gate: MCP rules unchanged, other tools confirmed
		// through the asker (bound later by chat) per tools.approval.
		auto, err := toolsApprovalAuto()
		if err != nil {
			return nil, err
		}
		asker = tools.NewBindableAsker()
		approver = newChatApprover(gate, auto, asker, workspace)
		// Memory writes of tainted sessions are confirmed at the same terminal.
		opts.MemoryConfirm = newChatMemoryConfirmer(asker).confirm
	}
	opts.Workspace, opts.RuleCWD = workspace, cwd
	// tools.allow_write applies to chat only: run (often started by a job
	// runner sharing this config) still needs an explicit --allow-write.
	// --read-only always wins.
	configWrite := false
	if opts.ChatMode {
		if configWrite, err = toolsAllowWrite(); err != nil {
			return nil, err
		}
	}
	opts.AllowWrite = (opts.AllowWrite || configWrite) && !opts.ReadOnly
	opts.Servers, opts.Hooks, opts.Approver = servers, nil, approver
	rt, err := Open(ctx, opts)
	if err != nil {
		return nil, err
	}
	rt.Asker, rt.WriteTools = asker, opts.AllowWrite
	return rt, nil
}

// toolsAllowWrite reads tools.allow_write (default false); a non-boolean
// value is a startup error instead of being guessed.
func toolsAllowWrite() (bool, error) {
	switch value := gkconfig.Get("tools.allow_write").(type) {
	case nil:
		return false, nil
	case bool:
		return value, nil
	default:
		return false, fmt.Errorf("tools.allow_write must be a boolean (true or false), got %v", value)
	}
}

// Open assembles one runtime.
func Open(ctx context.Context, opts Options) (*Runtime, error) {
	workspace, ruleCWD, allowWrite, servers := opts.Workspace, opts.RuleCWD, opts.AllowWrite, opts.Servers
	rs, err := loadRuntimeSettings(workspace, opts.Model)
	if err != nil {
		return nil, err
	}
	settings, taintTools, taintOnSpill := rs.models, rs.taintTools, rs.taintOnSpill
	memSetup, memPolicy, sessionsDir := rs.memSetup, rs.memPolicy, rs.sessionsDir
	provider, err := buildMainProvider(settings)
	if err != nil {
		return nil, err
	}
	contextWindow := appconfig.ContextWindowFor(settings.Main)
	// A bad proxy is a startup error rather than a silently missing web_fetch.
	if _, err := native.ParseFetchProxy(gkconfig.String("tools.web_fetch.proxy")); err != nil {
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
	// memory changes suag's own state too, so it is registered in every mode;
	// read-only modes keep get/search only (the tool refuses writes itself).
	// The runner and the provenance store are bound after personal.New.
	memHost := &memoryHost{}
	mem := newMemoryRuntime(memSetup, memPolicy, opts.MemoryReadOnly, opts.MemoryConfirm, memHost)
	capabilities.Registry.Register(mem.tool)
	// session_search / recall only read the session store and the saved
	// outputs, so they are registered in every mode as well; the ledger is
	// bound to the runner's store in personal.New.
	ledger := personal.NewLedger(sessionsDir)
	capabilities.Registry.Register(native.NewSessionSearchTool(ledger))
	capabilities.Registry.Register(native.NewRecallTool(ledger, inlineOutputBytes()))
	// Skills are scanned once per process; the skill tool (read-only, every
	// mode) and the prompt listing exist only when some skill was found.
	skillReg := buildDefaultSkillRegistry(workspace)
	if skillReg.Len() > 0 {
		capabilities.Registry.Register(native.NewSkillTool(skillReg, nativeEnv(workspace).RawBytes))
	}
	// suag_manifest is read-only local data, so it is registered in every
	// mode. It reports the live registry (MCP tools included) when called.
	userHome, _ := os.UserHomeDir()
	capabilities.Registry.Register(native.NewManifestTool(func(context.Context) (any, error) {
		return BuildManifest(ManifestInput{
			Version: buildVersion, Settings: settings, Registry: capabilities.Registry,
			MCPServers: append(appconfig.ReadMCPServerSettings(), servers...),
			SkillRoots: defaultSkillRoots(workspace, userHome), Skills: skillReg,
			Memory: memSetup, Policy: memPolicy,
			InlineBytes: rs.inlineBytes, SpillBytes: rs.spillBytes, SessionsDir: sessionsDir,
			TaintTools: taintTools, TaintOnSpill: taintOnSpill,
		}), nil
	}))
	// Tool registry stack, outer to inner: observe (added in personal.New) ->
	// visibility -> approval -> base registry. Unloaded deferred tools are
	// answered by visibility and never reach the approver.
	approval := tools.NewApproval(capabilities.Registry, opts.Approver)
	approval.SetDisabled(disabledToolHints(capabilities.Registry, opts))
	vis, err := newToolVisibility(capabilities.Registry, approval, contextWindow)
	if err != nil {
		_ = capabilities.Close()
		return nil, err
	}
	var toolRegistry agentcore.ToolRegistry = vis
	dbPath, err := appconfig.ResolveCoreStorePath()
	if err != nil {
		_ = capabilities.Close()
		return nil, fmt.Errorf("resolve personal session database: %w", err)
	}
	transcriptDir := ""
	if appconfig.TranscriptsEnabled() {
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
	// compact_model: summaries use that model (and its provider); unset means
	// the turn's own provider.
	var compactProvider agentcore.Provider
	compactModel := ""
	if settings.Compact != nil {
		built, err := appconfig.BuildProvider(*settings.Compact)
		if err != nil {
			_ = repo.Close()
			_ = capabilities.Close()
			return nil, fmt.Errorf("build compact model provider: %w", err)
		}
		compactProvider, compactModel = built, settings.Compact.Model
	}
	runner, err := personal.New(personal.Config{
		ContextWindow: contextWindow,
		// Experimental (R3 history folding A/B); undocumented, off by default.
		FoldEvery:       gkconfig.Int("context.fold_every", 0),
		FoldKeep:        gkconfig.Int("context.fold_keep", 4),
		CompactProvider: compactProvider,
		CompactModel:    compactModel,
		Plans:           plans,
		Ledger:          ledger,
		MemoryIndex:     mem.index,
		Workspace:       workspace,
		Mode:            runMode(allowWrite),
		TaintTools:      taintTools,
		TaintOnSpill:    taintOnSpill,
		Visibility:      vis,
		Price:           priceLookup(settings),
		// Tool outputs above tools.max_output_bytes go to
		// <SUAG_HOME>/sessions/<id>/outputs/ and the session keeps a ref.
		InlineOutputBytes: inlineOutputBytes(),
		Spiller:           personal.NewSessionSpiller(sessionsDir),
		Repository:        repo,
		Provider:          provider,
		Model:             settings.Main.Model,
		ProviderName:      settings.Main.Provider.ID,
		Tools:             toolRegistry,
		PromptBuilder:     staticPrompt(basePrompt()),
		// Environment and skills go into the segment context (design D1).
		EnvironmentSource: func() string { return environmentPrompt(opts) },
		SkillsSource:      func() string { return skillsPrompt(skillReg, readSkillPolicy()) },
		Hooks:             opts.Hooks,
		MaxSteps:          gkconfig.Int("max_steps", 100),
		RuleSource: func() (string, string, error) {
			home, err := appconfig.ResolveSuagHome()
			if err != nil {
				return "", "", err
			}
			return personal.LoadRules(ruleCWD, home, opts.NoProjectRules)
		},
		AppendSystemPrompt:    appendSystemPrompt(opts.AppendSystemPrompt),
		AppendSystemPromptSet: opts.ClearAppendSystemPrompt,
		TranscriptDir:         transcriptDir,
		LockRoot:              lockRoot,
		ProcessRoot:           processRoot,
	})
	if err != nil {
		_ = repo.Close()
		_ = capabilities.Close()
		return nil, err
	}
	memHost.bind(runner, repo)
	return &Runtime{Runner: runner, Registry: capabilities.Registry, repo: repo, capabilities: capabilities,
		ContextWindow: contextWindow, tools: toolRegistry}, nil
}

// buildMainProvider builds the main provider; with fallback_models the result
// is a Pool that tries main first, then each fallback in order (a Pool never
// switches once a stream has produced output).
func buildMainProvider(settings appconfig.ModelSettings) (agentcore.Provider, error) {
	main, err := appconfig.BuildProvider(settings.Main)
	if err != nil {
		return nil, err
	}
	if len(settings.Fallbacks) == 0 {
		return main, nil
	}
	providers := []llm.Provider{main}
	for i, spec := range settings.Fallbacks {
		fb, err := appconfig.BuildProvider(spec)
		if err != nil {
			return nil, fmt.Errorf("fallback_models[%d]: %w", i, err)
		}
		providers = append(providers, fb)
	}
	return llm.NewPool(providers), nil
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

func buildPersonalCapabilitiesWithServers(ctx context.Context, workspace string, allowWrite bool, servers []mcp.ServerSettings) (defaultCapabilities, error) {
	if allowWrite {
		return buildDefaultCapabilitiesWithServers(ctx, workspace, servers)
	}
	// Read-only: only the catalog's read tools, and no MCP (ACP read_only
	// sessions must not start MCP servers).
	registry := tools.NewRegistry()
	for _, tool := range native.BuildTools(nativeEnv(workspace), false) {
		registry.Register(tool)
	}
	return defaultCapabilities{Registry: registry}, nil
}

// CLI always exposes configured MCP tools; the dispatch gate enforces their permissions.
func buildPersonalCLICapabilities(ctx context.Context, workspace string, allowWrite bool, servers []mcp.ServerSettings) (defaultCapabilities, error) {
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

// priceLookup adapts the configured model prices for personal, which never
// reads global configuration itself.
func priceLookup(settings appconfig.ModelSettings) func(provider, model string) (personal.Price, bool) {
	lookup := settings.PriceLookup()
	return func(provider, model string) (personal.Price, bool) {
		p, ok := lookup(provider, model)
		return personal.Price{Input: p.Input, Output: p.Output, CacheRead: p.CacheRead, CacheWrite: p.CacheWrite}, ok
	}
}

// runMode names the run mode for turn_context events.
func runMode(allowWrite bool) string {
	if allowWrite {
		return personal.ModeAllowWrite
	}
	return personal.ModeReadOnly
}

// toolNameList reads a list of tool names; unset means def, an explicit empty
// list means none. Anything but a list of non-empty strings is a startup
// error.
func toolNameList(key string, def []string) ([]string, error) {
	raw := gkconfig.Get(key)
	if raw == nil {
		return def, nil
	}
	var items []any
	switch value := raw.(type) {
	case []string:
		for _, v := range value {
			items = append(items, v)
		}
	case []any:
		items = value
	default:
		return nil, fmt.Errorf("%s must be a list of tool names", key)
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		name, ok := item.(string)
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("%s must contain non-empty tool names, got %v", key, item)
		}
		names = append(names, strings.TrimSpace(name))
	}
	return names, nil
}
