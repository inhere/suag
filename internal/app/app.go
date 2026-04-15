package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	gkconfig "github.com/gookit/config/v2"

	appconfig "github.com/inhere/axon/internal/config"
	"github.com/inhere/axon/internal/host/gateway"
	"github.com/inhere/axon/internal/ops/cron"
	"github.com/inhere/axon/internal/ops/hitl"
	"github.com/inhere/axon/internal/orchestrator"
	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/internal/platform/memory"
	"github.com/inhere/axon/internal/platform/tools"
	"github.com/inhere/axon/internal/platform/tools/native"
	"github.com/inhere/axon/internal/platform/tracing"
	"github.com/inhere/axon/internal/team"
)

var ErrUnknownMode = errors.New("unknown mode")

type App struct {
	provider  llm.Provider
	registry  *tools.Registry
	orch      *orchestrator.Orchestrator
	bus       *gateway.EventBus
	hitlMgr   hitl.Manager
	tracer    tracing.Tracer
	scheduler *cron.Scheduler
	workspace string
}

func SetupLogging() {
	level := slog.LevelInfo
	if os.Getenv("AXON_DEBUG") != "" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}

func New() (*App, error) {
	workspace := appconfig.ResolveWorkspacePath()

	llmSettings := appconfig.ReadLLMProviderSettings()
	provider, err := appconfig.BuildLLMProviderFromSettings(llmSettings)
	if err != nil {
		return nil, err
	}
	slog.Info("llm provider configured", llmSettings.LogArgs()...)

	registry := tools.NewRegistry()
	for _, t := range native.NewFileTools(workspace) {
		registry.MustRegister(t)
	}
	registry.MustRegister(native.NewShellExecTool(workspace, 0))
	if serpKey := gkconfig.String("serpapi_key"); serpKey != "" {
		registry.MustRegister(native.NewWebSearchTool(serpKey))
	}

	bus := gateway.NewEventBus()
	tracer := tracing.NewInMemoryTracer(func(span *tracing.Span) {
		bus.Publish(gateway.Event{
			Type:    "span",
			TraceID: span.TraceID,
			AgentID: span.AgentID,
			Payload: span,
		})
	})

	hitlMgr := hitl.NewInMemoryManager(func(ticket *hitl.Ticket) {
		bus.Publish(gateway.Event{
			Type:    "hitl_ticket",
			AgentID: ticket.AgentID,
			Payload: ticket,
		})
		slog.Warn("HITL intervention required",
			"ticket", ticket.ID,
			"agent", ticket.AgentID,
			"reason", ticket.Reason,
		)
	})

	teamMgr := team.NewManager(provider, registry, tracer, hitlMgr, workspace, team.ManagerConfig{
		Runtime:          gkconfig.String("agent.runtime"),
		AgentcoreRunRoot: gkconfig.String("agentcore.run_root"),
		FailThreshold:    gkconfig.Int("hitl.fail_threshold", 3),
	})

	_ = memory.NewInMemStore(memory.InMemConfig{})

	orch := orchestrator.New(orchestrator.Config{
		LLM:                provider,
		Registry:           registry,
		Tracer:             tracer,
		HITL:               hitlMgr,
		Team:               teamMgr,
		Workspace:          workspace,
		MaxSteps:           gkconfig.Int("max_steps", 10),
		FailThreshold:      gkconfig.Int("hitl.fail_threshold", 3),
		UseAgentcore:       gkconfig.Bool("agentcore.enabled", false),
		SingleAgentRuntime: gkconfig.String("agent.runtime"),
		AgentcoreRunRoot:   gkconfig.String("agentcore.run_root"),
	})

	scheduler := cron.NewScheduler(orch, provider, registry)

	return &App{
		provider:  provider,
		registry:  registry,
		orch:      orch,
		bus:       bus,
		hitlMgr:   hitlMgr,
		tracer:    tracer,
		scheduler: scheduler,
		workspace: workspace,
	}, nil
}

func (a *App) Run(ctx context.Context, mode string) error {
	switch strings.TrimSpace(mode) {
	case "", "chat":
		a.RunChat(ctx)
		return nil
	case "server":
		return a.RunServer(ctx)
	default:
		return fmt.Errorf("%w %q", ErrUnknownMode, mode)
	}
}
