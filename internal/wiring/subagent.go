package wiring

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/tools"
	"github.com/inhere/suag/pkg/agentcore/tools/native"
)

// subAgents runs the agent tool's sub-agents (docs/design/2026-10-13-
// subagents-design.md). Each call builds a Runner of its own: its own store
// handle (a background sub-agent may outlive an ACP prompt's runtime) and
// provider (a shared llm.Pool would get its observer replaced), read-only
// tools by type, a sub-agent prompt and environment, and the agents.* limits.
type subAgents struct {
	workspace, ruleCWD string
	noProjectRules     bool
	ruleFile           string
	settings           appconfig.AgentSettings
}

var _ native.SubAgents = (*subAgents)(nil)

// subAgentTools are the catalog tools of each sub-agent type. recall is left
// out: it marks the session as having read external content, and a fresh
// sub-agent session has nothing of its own to recall.
var subAgentTools = map[string][]string{
	native.AgentExplore:  {"file_read", "file_list", "file_grep", "file_find"},
	native.AgentResearch: {"file_read", "file_list", "file_grep", "file_find", "web_fetch", "web_search"},
}

func (s *subAgents) Scope(ctx context.Context) (string, string) { return personal.CurrentTurn(ctx) }

func (s *subAgents) RunAgent(ctx context.Context, req native.AgentRequest) (native.AgentReport, error) {
	child, closeChild, err := s.build(req.Type)
	if err != nil {
		return native.AgentReport{Stop: "failed"}, err
	}
	defer closeChild()
	var progress personal.EmitFunc
	if !req.Background {
		progress = personal.AgentProgress(ctx)
	}
	res, runErr := personal.RunChild(ctx, child, personal.ChildRequest{
		ParentSession: req.ParentSession, ParentTurn: req.ParentTurn, ParentCall: req.CallID,
		Type: req.Type, Description: req.Description, Prompt: req.Prompt,
		Timeout: s.settings.Timeout, Emit: progress,
	})
	rep := native.AgentReport{SessionID: res.SessionID, Report: res.Report, Stop: res.Stop, Steps: res.Steps,
		ToolCalls: res.ToolCalls, Usage: res.Usage, Duration: res.Duration, Tainted: res.Tainted}
	// A background report reaches the parent through a notice or the task
	// tool, which carry no taint of their own: mark the parent here.
	if req.Background && res.Tainted && req.ParentSession != "" {
		if err := markTainted(context.WithoutCancel(ctx), req.ParentSession, res.SessionID); err != nil {
			return rep, err
		}
	}
	return rep, runErr
}

// markTainted marks session as having read external content (through the
// sub-agent session by), unless it already is.
func markTainted(ctx context.Context, session, by string) error {
	dbPath, err := appconfig.ResolveCoreStorePath()
	if err != nil {
		return err
	}
	repo, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = repo.Close() }()
	_, err = repo.PatchSessionMetaIfAbsent(ctx, session, "tainted_at", map[string]any{
		"tainted_at": time.Now().UTC().Format(time.RFC3339), "tainted_by": "agent:" + by,
	})
	return err
}

// build assembles the Runner of one sub-agent run; the func closes it.
func (s *subAgents) build(kind string) (*personal.Runner, func(), error) {
	rs, err := loadRuntimeSettings(s.workspace, s.settings.Model, "")
	if err != nil {
		return nil, nil, fmt.Errorf("agents.model: %w", err)
	}
	provider, err := buildMainProvider(rs.models)
	if err != nil {
		return nil, nil, err
	}
	registry := tools.NewRegistry()
	for _, tool := range native.BuildTools(nativeEnv(s.workspace), false) {
		for _, name := range subAgentTools[kind] {
			if tool.Name() == name {
				registry.Register(tool)
			}
		}
	}
	// Every tool here only reads; anything else is not there to call.
	approval := tools.NewApproval(registry, tools.ApproverFunc(func(context.Context, agentcore.ToolCall, agentcore.Tool) error { return nil }))
	approval.SetDisabled(subAgentDisabledHints(registry))
	dbPath, err := appconfig.ResolveCoreStorePath()
	if err != nil {
		return nil, nil, err
	}
	repo, err := store.Open(dbPath)
	if err != nil {
		return nil, nil, err
	}
	runner, err := personal.New(personal.Config{
		Repository: repo, Provider: provider, Tools: approval,
		Model: rs.models.Main.Model, ProviderName: rs.models.Main.Provider.ID,
		ContextWindow: appconfig.ContextWindowFor(rs.models.Main),
		Price:         priceLookup(rs.models),
		Workspace:     s.workspace, Version: buildVersion, Mode: runMode(false),
		TaintTools: rs.taintTools, TaintOnSpill: rs.taintOnSpill,
		InlineOutputBytes: inlineOutputBytes(), Spiller: personal.NewSessionSpiller(rs.sessionsDir),
		MinimizeOutput: minimizeOutput(s.workspace), PrefetchTools: prefetchTools(registry),
		PromptBuilder:     staticPrompt(subAgentPrompt()),
		EnvironmentSource: func() string { return subAgentEnvironment(s.workspace, kind) },
		RuleSource: func() (string, string, error) {
			home, err := appconfig.ResolveSuagHome()
			if err != nil {
				return "", "", err
			}
			return personal.LoadRules(s.ruleCWD, home, s.noProjectRules, s.ruleFile)
		},
		MaxSteps: s.settings.MaxSteps, MaxRunTokens: s.settings.MaxTokens, RunTimeout: s.settings.Timeout,
	})
	if err != nil {
		_ = repo.Close()
		return nil, nil, err
	}
	return runner, func() { _ = repo.Close() }, nil
}

// subAgentDisabledHints: a catalog tool a sub-agent lacks is refused with a
// hint to report what it would need instead of asking the user.
func subAgentDisabledHints(registry *tools.Registry) map[string]string {
	hints := map[string]string{}
	for _, spec := range native.Catalog() {
		if _, err := registry.Get(spec.Name); err != nil {
			hints[spec.Name] = "sub-agents only read: say in your report what should be run or changed, and why"
		}
	}
	return hints
}

// subAgentPrompt is the system prompt of every sub-agent (one per process,
// so it is cache-stable like basePrompt).
func subAgentPrompt() string {
	var b strings.Builder
	b.WriteString("You are a suag sub-agent: another agent handed you one task. Work it out with your read-only tools and answer with a report for that agent; nobody else reads it, and you cannot ask questions.\n\n")
	b.WriteString("# Working conventions\n")
	b.WriteString("- Search before reading: file_grep / file_find to locate, then file_read the parts that matter. Put independent reads and searches in the same response.\n")
	b.WriteString("- Stay on the task; stop as soon as you can answer it.\n")
	b.WriteString("- Your final answer is the report: the conclusion first, then the evidence (file:line, short quotes), then what you could not settle. Keep it under about 4000 characters; no greetings, no restating the task.\n")
	b.WriteString("- You cannot write files or run commands; when the task needs that, say in the report what should be done and why.\n")
	b.WriteString("- Answer in the language of the task.\n")
	return strings.TrimRight(b.String(), "\n")
}

// subAgentEnvironment is a sub-agent's environment section: always read-only,
// never telling it to have the user enable writes.
func subAgentEnvironment(workspace, kind string) string {
	var b strings.Builder
	b.WriteString("# Environment\n")
	fmt.Fprintf(&b, "- OS: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&b, "- Workspace (file tools are confined to it): %s\n", workspace)
	if roots, _ := readRoots(); len(roots) > 0 {
		b.WriteString("- Read-only roots outside the workspace (the file tools accept absolute paths under them):\n")
		for _, r := range roots {
			fmt.Fprintf(&b, "  - %s: %s\n", r.Label, r.Path)
		}
	}
	fmt.Fprintf(&b, "- Sub-agent type: %s; read-only.", kind)
	return b.String()
}
