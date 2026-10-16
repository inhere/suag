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
	"github.com/inhere/suga/internal/adddir"
	appconfig "github.com/inhere/suga/internal/config"
	"github.com/inhere/suga/internal/personal"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/llm"
	"github.com/inhere/suga/pkg/agentcore/tasks"
	"github.com/inhere/suga/pkg/agentcore/tools"
	"github.com/inhere/suga/pkg/agentcore/tools/mcp"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

// Options carries everything needed to assemble one runtime.
type Options struct {
	// Workspace is the tool workspace root; RuleCWD is where project rules are read.
	// Both are filled by OpenCLI.
	Workspace string
	RuleCWD   string
	Model     string
	// Thinking overrides the main model's thinking level (--thinking).
	Thinking string
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
	// get/search. The zero value allows memory writes: memory is suga's own
	// state, not the workspace, so a plain run may write it.
	MemoryReadOnly bool
	// Tasks runs shell_exec with background: true and backs the task tool
	// (write modes only); nil leaves background tasks off. The host owns it
	// and closes it (killing its tasks) when the session or process ends.
	Tasks *tasks.Manager
	// MaxSteps overrides the config's max_steps for the main agent's turns
	// (--max-steps, run only); 0 uses SUGA_MAX_STEPS, then max_steps.
	MaxSteps int
	// MaxOutputTokens overrides the configured output token limit for this
	// invocation (--max-output-tokens); 0 uses SUGA_MAX_OUTPUT_TOKENS, then
	// providers.<id>.models.<m>.max_output_tokens /
	// providers.<id>.max_output_tokens (see appconfig.ResolveMaxOutputTokens).
	MaxOutputTokens int
	// MemoryConfirm confirms memory writes of tainted sessions (ACP: the turn
	// gate's permission request; chat: set by OpenCLI to ask at the terminal).
	// nil (run) refuses them.
	MemoryConfirm MemoryConfirmFunc
	// AddDirs are the additional directories this invocation asks for
	// (--add-dir, SUGA_ADD_DIRS, already parsed by the front end). A session
	// keeps them in its metadata; ApplyAddDirs publishes the merged set.
	AddDirs []adddir.Dir
	// sessionDirs is the live set the tools and the environment prompt read. It
	// is created by OpenCLI so the chat approver and the tools share one set; nil
	// (a runtime assembled directly) means no additional directory.
	sessionDirs *native.AddDirs
}

// Runtime is one assembled personal runner and the resources it owns.
type Runtime struct {
	Runner *personal.Runner
	// Registry is the base tool registry (lets ACP tell MCP tools apart).
	Registry *tools.Registry
	// ContextWindow is the window of the model actually used by the provider.
	ContextWindow int
	// Thinking is the main model's effective thinking level ("" = the
	// provider's default).
	Thinking string
	// Asker (chat mode only, else nil) is where the chat approver asks its
	// questions; chat binds its terminal input to it. Unbound, every tool that
	// needs a confirmation is refused.
	Asker *tools.BindableAsker
	// WriteTools reports whether workspace write and shell tools are exposed.
	WriteTools bool
	// AddDirs is the session's additional-directory set (--add-dir): the tools
	// read it at every call, so the front end fills it once the session is known
	// (ApplyAddDirs).
	AddDirs *native.AddDirs

	repo         *store.Store
	capabilities defaultCapabilities
	// sessionDirs is the handle on the session's added directories: ApplyAddDirs
	// publishes them through it, chat's :add-dir/:rm-dir commands and the
	// outside-directory question change them through it too, so the metadata and
	// the live set stay in step.
	sessionDirs *SessionAddDirs
	// local is the runner of chat's local commands ("!cmd"/"!!cmd"): the shell,
	// workspace and output limits are the ones the shell tools use, so a command
	// a person types there behaves like one the model runs.
	local *native.LocalCommand
	// tools is the registry stack the runner calls (visibility -> approval
	// -> base); kept for tests.
	tools agentcore.ToolRegistry
}

// LocalCommands is the runner of chat's local commands ("!cmd"/"!!cmd"): chat
// runs a line typed at the prompt through it. It is nil for a nil runtime.
func (rt *Runtime) LocalCommands() *native.LocalCommand {
	if rt == nil {
		return nil
	}
	return rt.local
}

// SessionAddDirs is the runtime's handle on the session's added directories
// (--add-dir): the chat front end lists and changes them through it after the
// session is open (ApplyAddDirs).
func (p *Runtime) SessionAddDirs() *SessionAddDirs {
	if p == nil {
		return nil
	}
	return p.sessionDirs
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
	// One additional-directory set per runtime, shared by the tools, the
	// environment prompt and the chat approver; the tools read it per call, so
	// the session opened later can still fill it (ApplyAddDirs).
	dirs := native.NewAddDirs()
	opts.sessionDirs = dirs
	var approver tools.Approver = gate
	var asker *tools.BindableAsker
	var chatGate *chatApprover
	if opts.ChatMode {
		// chat wraps the gate: MCP rules unchanged, other tools confirmed
		// through the asker (bound later by chat) per tools.approval.
		auto, err := toolsApprovalAuto()
		if err != nil {
			return nil, err
		}
		asker = tools.NewBindableAsker()
		// The outside-directory question needs the tool's static read roots (a
		// path under one needs no authorization) and the session's added-directory
		// set, both read once here.
		roots, _ := readRoots()
		chatGate = newChatApprover(chatApproverOptions{
			gate: gate, auto: auto, asker: asker, workspace: workspace, dirs: dirs,
			readRoots: native.RootPaths(roots), readOnly: opts.ReadOnly,
			askOutside: appconfig.ChatAskOutsideDirs(),
		})
		approver = chatGate
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
	if chatGate != nil {
		// The outside-directory question authorizes a path through the same
		// handle ApplyAddDirs publishes the session's directories with.
		chatGate.SetAddDirs(rt.sessionDirs)
	}
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
	// The session's additional directories (--add-dir): the tools read the set at
	// every call, so the requested ones are published here already (a new
	// session's first segment lists them) and ApplyAddDirs merges the session's
	// own ones in once it is open. A read-only run exposes the read-only view.
	dirs := opts.sessionDirs
	if dirs == nil {
		dirs = native.NewAddDirs()
	}
	dirs.Set(nativeAddDirs(adddir.Effective(opts.AddDirs, opts.ReadOnly)))
	opts.sessionDirs = dirs
	rs, err := loadRuntimeSettings(workspace, opts.Model, opts.Thinking)
	if err != nil {
		return nil, err
	}
	// Project rules are one file per level, named by rules.file; a bad name is a
	// startup error rather than a silently skipped rule file.
	ruleFile, err := appconfig.ReadRuleFileName()
	if err != nil {
		return nil, err
	}
	// Step limit of one main-agent turn: --max-steps > SUGA_MAX_STEPS > max_steps.
	maxSteps, err := appconfig.ReadMaxSteps(opts.MaxSteps)
	if err != nil {
		return nil, err
	}
	// Output token limit of the main model's requests: --max-output-tokens >
	// SUGA_MAX_OUTPUT_TOKENS > models.<m>.max_output_tokens >
	// providers.<id>.max_output_tokens (0 = the provider's own default).
	maxOutputTokens, err := appconfig.ResolveMaxOutputTokens(opts.MaxOutputTokens, rs.models.Main)
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
		capabilities, err = buildPersonalCLICapabilities(ctx, workspace, dirs, allowWrite, servers)
	} else {
		capabilities, err = buildPersonalCapabilitiesWithServers(ctx, workspace, dirs, allowWrite, servers)
	}
	if err != nil {
		return nil, err
	}
	// Background tasks: shell commands in write modes (they need
	// shell_exec); sub-agents (below) in every mode, so the task tool and the
	// notices are there in read-only sessions too.
	if opts.Tasks != nil {
		if t, err := capabilities.Registry.Get("shell_exec"); err == nil && allowWrite {
			if shell, ok := t.(*native.ShellExecTool); ok {
				shell.SetTasks(opts.Tasks)
			}
		}
		capabilities.Registry.Register(native.NewTaskTool(opts.Tasks, nativeEnv(workspace, dirs).ShellTimeoutMax))
	}
	// agent hands a task to a read-only sub-agent (every mode; agents.enabled).
	agentSettings, err := appconfig.ReadAgentSettings()
	if err != nil {
		_ = capabilities.Close()
		return nil, err
	}
	if agentSettings.Enabled {
		agentTool := native.NewAgentTool(&subAgents{workspace: workspace, ruleCWD: ruleCWD, mainModel: opts.Model,
			noProjectRules: opts.NoProjectRules, ruleFile: ruleFile, settings: agentSettings, addDirs: dirs})
		if opts.Tasks != nil {
			agentTool.SetTasks(opts.Tasks)
		}
		capabilities.Registry.Register(agentTool)
	}
	// update_plan only writes session state, so it is registered in every mode
	// (including read-only). The recorder is bound to the runner in personal.New.
	plans := personal.NewPlanRecorder()
	capabilities.Registry.Register(native.NewUpdatePlanTool(plans))
	// memory changes suga's own state too, so it is registered in every mode;
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
	// Skills are scanned once per process (the built-in ones are released into
	// <SUGA_HOME>/skills first); the skill tool (read-only, every mode) and the
	// prompt listing exist only when some skill was found.
	skillReg := buildDefaultSkillRegistry(workspace)
	if skillReg.Len() > 0 {
		capabilities.Registry.Register(native.NewSkillTool(skillReg, nativeEnv(workspace, dirs).RawBytes))
	}
	// suga_manifest is read-only local data, so it is registered in every
	// mode. It reports the live registry (MCP tools included) when called.
	capabilities.Registry.Register(native.NewManifestTool(func(context.Context) (any, error) {
		return BuildManifest(ManifestInput{
			Version: buildVersion, Settings: settings, Registry: capabilities.Registry,
			MCPServers: append(appconfig.ReadMCPServerSettings(), servers...),
			SkillRoots: skillRoots(workspace), Skills: skillReg,
			Memory: memSetup, Policy: memPolicy,
			InlineBytes: rs.inlineBytes, SpillBytes: rs.spillBytes, SessionsDir: sessionsDir,
			TaintTools: taintTools, TaintOnSpill: taintOnSpill,
		}), nil
	}))
	// Tool registry stack, outer to inner: observe (added in personal.New) ->
	// visibility -> approval -> base registry. Unloaded deferred tools are
	// answered by visibility and never reach the approver.
	// Dangerous deletes are stopped before any approver (guardDangerous).
	approval := tools.NewApproval(capabilities.Registry, guardDangerous(opts.Approver))
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
		home, err := appconfig.ResolveSugaHome()
		if err != nil {
			_ = capabilities.Close()
			return nil, fmt.Errorf("resolve transcript root: %w", err)
		}
		transcriptDir = filepath.Join(home, "transcripts")
	}
	home, err := appconfig.ResolveSugaHome()
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
	// A nil manager must stay a nil interface (no tasks).
	var taskNotices personal.TaskNotices
	if opts.Tasks != nil {
		taskNotices = opts.Tasks
	}
	runner, err := personal.New(personal.Config{
		Tasks:         taskNotices,
		ContextWindow: contextWindow,
		// History folding of the request view (context.fold_every/fold_keep).
		FoldEvery:       rs.fold.Every,
		FoldKeep:        rs.fold.Keep,
		CompactProvider: compactProvider,
		CompactModel:    compactModel,
		Plans:           plans,
		Ledger:          ledger,
		MemoryIndex:     mem.index,
		Workspace:       workspace,
		Version:         buildVersion,
		Mode:            runMode(allowWrite),
		// chat shows what a file tool changed (chat.diff): its tool_call events then
		// carry the change on disk (internal/diffview). run, ACP and stream-json keep
		// file text out of their events; ACP builds its own diff for its client.
		DiffPreview:  opts.ChatMode && appconfig.ChatDiffEnabled(),
		TaintTools:   taintTools,
		TaintOnSpill: taintOnSpill,
		Visibility:   vis,
		Price:        priceLookup(settings),
		// Tool outputs above tools.max_output_bytes go to
		// <SUGA_HOME>/sessions/<id>/outputs/ and the session keeps a ref.
		InlineOutputBytes: inlineOutputBytes(),
		Spiller:           personal.NewSessionSpiller(sessionsDir),
		MinimizeOutput:    minimizeOutput(workspace),
		PrefetchTools:     prefetchTools(capabilities.Registry),
		Repository:        repo,
		Provider:          provider,
		Model:             settings.Main.Model,
		ProviderName:      settings.Main.Provider.ID,
		Tools:             toolRegistry,
		PromptBuilder:     staticPrompt(basePrompt()),
		// Environment and skills go into the segment context (design D1).
		EnvironmentSource: func() string { return environmentPrompt(opts) },
		// The turn_context event records the session's added directories.
		AddDirs: func() []string { return dirs.List() },
		SkillsSource: func() string {
			text, _ := skillsPrompt(skillReg, readSkillPolicy())
			return text
		},
		SkillsStats: func() personal.SkillsStats {
			_, stats := skillsPrompt(skillReg, readSkillPolicy())
			return stats
		},
		Hooks:    opts.Hooks,
		MaxSteps: maxSteps,
		// The output limit of the main model's requests: the CLI flag and the
		// environment override the configured value.
		MaxOutputTokens: maxOutputTokens,
		RuleSource: func() (string, string, error) {
			home, err := appconfig.ResolveSugaHome()
			if err != nil {
				return "", "", err
			}
			return personal.LoadRules(ruleCWD, home, opts.NoProjectRules, ruleFile)
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
	rt := &Runtime{Runner: runner, Registry: capabilities.Registry, repo: repo, capabilities: capabilities,
		ContextWindow: contextWindow, Thinking: settings.Main.ThinkingLevel(), tools: toolRegistry,
		AddDirs: dirs}
	// The runner of chat's local commands ("!cmd"/"!!cmd"): they run like
	// shell_exec ones (same shell, workspace, environment and output rules) but
	// outside the tools and every turn - chat never asks for them (用户 2026-10-14).
	env := nativeEnv(workspace, dirs)
	rt.local = native.NewLocalCommand(native.LocalCommandOptions{
		Workspace: workspace, Shell: env.Shell, Timeout: env.ShellTimeoutMax,
		RawBytes: env.RawBytes, InlineBytes: env.InlineBytes, Minimize: minimizeOutput(workspace),
	})
	// The added directories of the session being served (--add-dir): the front
	// end fills the set once the session is open (ApplyAddDirs) through this
	// handle, which chat's commands and the outside-directory question share.
	rt.sessionDirs = newSessionAddDirs(runner, dirs, opts.AddDirs, opts.ReadOnly, workspace)
	return rt, nil
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
	return os.Getenv("SUGA_APPEND_SYSTEM_PROMPT")
}

func buildPersonalCapabilities(workspace string, dirs *native.AddDirs, allowWrite bool) (defaultCapabilities, error) {
	return buildPersonalCapabilitiesWithServers(context.Background(), workspace, dirs, allowWrite, nil)
}

func buildPersonalCapabilitiesWithServers(ctx context.Context, workspace string, dirs *native.AddDirs, allowWrite bool, servers []mcp.ServerSettings) (defaultCapabilities, error) {
	if allowWrite {
		return buildDefaultCapabilitiesWithServers(ctx, workspace, dirs, servers)
	}
	// Read-only: only the catalog's read tools, and no MCP (ACP read_only
	// sessions must not start MCP servers).
	registry := tools.NewRegistry()
	for _, tool := range native.BuildTools(nativeEnv(workspace, dirs), false) {
		registry.Register(tool)
	}
	return defaultCapabilities{Registry: registry}, nil
}

// CLI always exposes configured MCP tools; the dispatch gate enforces their permissions.
func buildPersonalCLICapabilities(ctx context.Context, workspace string, dirs *native.AddDirs, allowWrite bool, servers []mcp.ServerSettings) (defaultCapabilities, error) {
	if allowWrite {
		return buildDefaultCapabilitiesWithServers(ctx, workspace, dirs, servers)
	}
	built, err := buildPersonalCapabilitiesWithServers(ctx, workspace, dirs, false, nil)
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
