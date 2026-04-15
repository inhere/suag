// Command axon is the main entry point for the axon AI agent orchestration engine.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/gookit/config/v2"
	"github.com/gookit/config/v2/yaml"
	"github.com/gookit/ini/v2/dotenv"

	"github.com/inhere/axon/internal/host/acpbridge"
	"github.com/inhere/axon/internal/host/gateway"
	"github.com/inhere/axon/internal/host/intrunner"
	"github.com/inhere/axon/internal/host/session"
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

func main() {
	setupLogging()
	loadConfig()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	app, err := buildApp()
	if err != nil {
		slog.Error("failed to build app", "err", err)
		os.Exit(1)
	}

	// Determine run mode from args.
	mode := "chat"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	switch mode {
	case "server":
		runServer(ctx, app)
	case "chat":
		runChat(ctx, app)
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q. Usage: axon [chat|server]\n", mode)
		os.Exit(1)
	}
}

// ─── app wiring ───────────────────────────────────────────────────────────────

type app struct {
	provider  llm.Provider
	registry  *tools.Registry
	orch      *orchestrator.Orchestrator
	bus       *gateway.EventBus
	hitlMgr   hitl.Manager
	tracer    tracing.Tracer
	scheduler *cron.Scheduler
	workspace string
}

func buildApp() (*app, error) {
	workspace := config.String("workspace", "~/.axon/workspace")
	if workspace == "" {
		var err error
		workspace, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working dir: %w", err)
		}
	}

	// ── LLM provider ──────────────────────────────────────────────────────────
	llmSettings := readLLMProviderSettings()
	provider, err := buildLLMProviderFromSettings(llmSettings)
	if err != nil {
		return nil, err
	}
	slog.Info("llm provider configured", llmSettings.logArgs()...)

	// ── Tool registry ─────────────────────────────────────────────────────────
	registry := tools.NewRegistry()
	for _, t := range native.NewFileTools(workspace) {
		registry.MustRegister(t)
	}
	registry.MustRegister(native.NewShellExecTool(workspace, 0))
	if serpKey := config.String("serpapi_key"); serpKey != "" {
		registry.MustRegister(native.NewWebSearchTool(serpKey))
	}

	// ── Tracing ───────────────────────────────────────────────────────────────
	bus := gateway.NewEventBus()
	tracer := tracing.NewInMemoryTracer(func(span *tracing.Span) {
		bus.Publish(gateway.Event{
			Type:    "span",
			TraceID: span.TraceID,
			AgentID: span.AgentID,
			Payload: span,
		})
	})

	// ── HITL ──────────────────────────────────────────────────────────────────
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

	// ── Team manager ──────────────────────────────────────────────────────────
	teamMgr := team.NewManager(provider, registry, tracer, hitlMgr, workspace, team.ManagerConfig{
		Runtime:          config.String("agent.runtime"),
		AgentcoreRunRoot: config.String("agentcore.run_root"),
		FailThreshold:    config.Int("hitl.fail_threshold", 3),
	})

	// ── Memory (SQLite if path configured, else in-memory) ───────────────────
	_ = memory.NewInMemStore(memory.InMemConfig{}) // default; orchestrator creates per-task

	// ── Orchestrator ──────────────────────────────────────────────────────────
	orch := orchestrator.New(orchestrator.Config{
		LLM:                provider,
		Registry:           registry,
		Tracer:             tracer,
		HITL:               hitlMgr,
		Team:               teamMgr,
		Workspace:          workspace,
		MaxSteps:           config.Int("max_steps", 10),
		FailThreshold:      config.Int("hitl.fail_threshold", 3),
		UseAgentcore:       config.Bool("agentcore.enabled", false),
		SingleAgentRuntime: config.String("agent.runtime"),
		AgentcoreRunRoot:   config.String("agentcore.run_root"),
	})

	// ── Cron scheduler ────────────────────────────────────────────────────────
	scheduler := cron.NewScheduler(orch, provider, registry)

	return &app{
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

func buildLLMProvider() (llm.Provider, error) {
	return buildLLMProviderFromSettings(readLLMProviderSettings())
}

// ─── run modes ────────────────────────────────────────────────────────────────

func runServer(ctx context.Context, a *app) {
	sessionStore, err := session.NewSQLiteStore(resolveSessionDBPath())
	if err != nil {
		slog.Error("failed to initialize session store", "err", err)
		return
	}
	defer func() {
		if err := sessionStore.Close(); err != nil {
			slog.Warn("failed to close session store", "err", err)
		}
	}()

	sessionRunner, err := buildSessionRunner(a)
	if err != nil {
		slog.Error("failed to build session runner", "err", err)
		return
	}

	sessionSvc := session.NewService(sessionStore, a.bus, sessionRunner, session.ServiceConfig{})
	h := gateway.NewHandler(a.orch, sessionStore, sessionSvc, a.bus, a.hitlMgr)
	addr := config.String("server.addr", ":8180")
	if addr == "" {
		addr = ":8180"
	}
	srv := gateway.NewServer(addr, h, a.bus)

	// Start cron scheduler in background.
	go a.scheduler.Start(ctx)

	if err := srv.Start(ctx); err != nil {
		slog.Error("server stopped", "err", err)
	}
}

func resolveSessionDBPath() string {
	raw := strings.TrimSpace(config.String("database_url"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if raw == "" {
		return filepath.Join("db", "axon.db")
	}
	if strings.HasPrefix(raw, "sqlite://") {
		raw = strings.TrimPrefix(raw, "sqlite://")
		if idx := strings.Index(raw, "?"); idx >= 0 {
			raw = raw[:idx]
		}
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return filepath.Join("db", "axon.db")
	}
	return filepath.Clean(filepath.FromSlash(raw))
}

func buildSessionRunner(a *app) (session.Runner, error) {
	mode := strings.TrimSpace(config.String("host.acp.mode"))
	if mode == "" {
		return intrunner.New(intrunner.Config{
			Provider:      a.provider,
			Registry:      a.registry,
			Tracer:        a.tracer,
			HITL:          a.hitlMgr,
			RunRoot:       filepath.Join(a.workspace, ".agentcore", "host"),
			Runtime:       config.String("agent.runtime"),
			FailThreshold: config.Int("hitl.fail_threshold", 3),
			MaxSteps:      config.Int("max_steps", 10),
		})
	}

	adapterName := strings.TrimSpace(config.String("host.acp.adapter"))
	if adapterName == "" {
		adapterName = "codex"
	}

	localCommand := strings.Fields(strings.TrimSpace(config.String("host.acp.local_command")))
	acpCommand := strings.Fields(strings.TrimSpace(config.String("host.acp.acp_command")))
	if mode == "codex_app_server" && len(acpCommand) == 0 {
		slog.Info("host acp acp_command not configured, using codex app-server default")
	}

	// The server keeps ACP bridge selection at the edge and only passes the
	// resulting narrow session.Runner into the host runtime.
	return acpbridge.NewRunner(acpbridge.FactoryConfig{
		Mode:                mode,
		AdapterName:         adapterName,
		Workspace:           resolveWorkspacePath(),
		StubRemoteAgentName: strings.TrimSpace(config.String("host.acp.stub_remote_agent_name")),
		LocalCommand: acpbridge.CommandConfig{
			Command: localCommand,
		},
		ACPCommand: acpbridge.CommandConfig{
			Command: acpCommand,
		},
	})
}

func resolveWorkspacePath() string {
	workspace := config.String("workspace", "~/.axon/workspace")
	if workspace == "" {
		workspace, _ = os.Getwd()
	}
	return workspace
}

func runChat(ctx context.Context, a *app) {
	fmt.Println("axon chat mode — type your task and press Enter. Ctrl+C to exit.")
	fmt.Println("Commands: :quit, :hitl (list pending tickets), :respond <id> <instruction>")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		switch {
		case line == ":quit":
			return
		case line == ":hitl":
			tickets := a.hitlMgr.ListPending()
			if len(tickets) == 0 {
				fmt.Println("No pending HITL tickets.")
			}
			for _, t := range tickets {
				fmt.Printf("  [%s] agent=%s reason=%s\n", t.ID, t.AgentID, t.Reason)
			}
		case strings.HasPrefix(line, ":respond "):
			parts := strings.SplitN(line[9:], " ", 2)
			if len(parts) < 2 {
				fmt.Println("Usage: :respond <ticket-id> <instruction>")
				continue
			}
			if err := a.hitlMgr.Respond(parts[0], parts[1]); err != nil {
				fmt.Println("Error:", err)
			} else {
				fmt.Println("Instruction delivered.")
			}
		default:
			fmt.Println("Running task...")
			result, err := a.orch.RunTask(ctx, line)
			if err != nil {
				fmt.Println("Error:", err)
				continue
			}
			fmt.Println("\n─── Result ───────────────────────────────────────")
			fmt.Println(result.Output)
			fmt.Println("──────────────────────────────────────────────────")
		}
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func setupLogging() {
	level := slog.LevelInfo
	if os.Getenv("AXON_DEBUG") != "" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}

func loadConfig() {
	// config.SetEnvPrefix("AXON")
	dotenv.LoadExists(".")

	config.AddDriver(yaml.Driver)
	err := config.LoadExists("./config/config.yaml", "~/.axon/config.yaml", "config.yaml")
	if err != nil {
		slog.Error("loading config file", "err", err)
	}
}
