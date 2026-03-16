// Command kagent is the main entry point for the kagent AI agent orchestration engine.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/viper"

	"github.com/kagent/kagent/internal/cron"
	"github.com/kagent/kagent/internal/gateway"
	"github.com/kagent/kagent/internal/hitl"
	"github.com/kagent/kagent/internal/llm"
	"github.com/kagent/kagent/internal/memory"
	"github.com/kagent/kagent/internal/orchestrator"
	"github.com/kagent/kagent/internal/team"
	"github.com/kagent/kagent/internal/tools"
	"github.com/kagent/kagent/internal/tools/native"
	"github.com/kagent/kagent/internal/tracing"
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
		fmt.Fprintf(os.Stderr, "unknown mode %q. Usage: kagent [chat|server]\n", mode)
		os.Exit(1)
	}
}

// ─── app wiring ───────────────────────────────────────────────────────────────

type app struct {
	orch      *orchestrator.Orchestrator
	bus       *gateway.EventBus
	hitlMgr   hitl.Manager
	tracer    tracing.Tracer
	scheduler *cron.Scheduler
}

func buildApp() (*app, error) {
	workspace := viper.GetString("workspace")
	if workspace == "" {
		var err error
		workspace, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working dir: %w", err)
		}
	}

	// ── LLM provider ──────────────────────────────────────────────────────────
	provider, err := buildLLMProvider()
	if err != nil {
		return nil, err
	}

	// ── Tool registry ─────────────────────────────────────────────────────────
	registry := tools.NewRegistry()
	for _, t := range native.NewFileTools(workspace) {
		registry.MustRegister(t)
	}
	registry.MustRegister(native.NewShellExecTool(workspace, 0))
	if serpKey := viper.GetString("serpapi_key"); serpKey != "" {
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
	teamMgr := team.NewManager(provider, registry, tracer, hitlMgr, workspace)

	// ── Memory (SQLite if path configured, else in-memory) ───────────────────
	_ = memory.NewInMemStore(memory.InMemConfig{}) // default; orchestrator creates per-task

	// ── Orchestrator ──────────────────────────────────────────────────────────
	orch := orchestrator.New(orchestrator.Config{
		LLM:       provider,
		Registry:  registry,
		Tracer:    tracer,
		HITL:      hitlMgr,
		Team:      teamMgr,
		Workspace: workspace,
		MaxSteps:  viper.GetInt("max_steps"),
	})

	// ── Cron scheduler ────────────────────────────────────────────────────────
	scheduler := cron.NewScheduler(orch, provider, registry)

	return &app{
		orch:      orch,
		bus:       bus,
		hitlMgr:   hitlMgr,
		tracer:    tracer,
		scheduler: scheduler,
	}, nil
}

func buildLLMProvider() (llm.Provider, error) {
	prov := viper.GetString("llm.provider")
	switch prov {
	case "claude":
		key := viper.GetString("llm.api_key")
		if key == "" {
			key = os.Getenv("ANTHROPIC_API_KEY")
		}
		return llm.NewClaudeProvider(llm.ClaudeConfig{
			APIKey: key,
			Model:  viper.GetString("llm.model"),
		}), nil
	case "deepseek":
		key := viper.GetString("llm.api_key")
		if key == "" {
			key = os.Getenv("DEEPSEEK_API_KEY")
		}
		return llm.NewDeepSeekProvider(key, viper.GetString("llm.model")), nil
	default: // "openai"
		key := viper.GetString("llm.api_key")
		if key == "" {
			key = os.Getenv("OPENAI_API_KEY")
		}
		return llm.NewOpenAIProvider(llm.OpenAIConfig{
			APIKey:  key,
			Model:   viper.GetString("llm.model"),
			BaseURL: viper.GetString("llm.base_url"),
		}), nil
	}
}

// ─── run modes ────────────────────────────────────────────────────────────────

func runServer(ctx context.Context, a *app) {
	h := gateway.NewHandler(a.orch, a.bus, a.hitlMgr)
	addr := viper.GetString("server.addr")
	if addr == "" {
		addr = ":8080"
	}
	srv := gateway.NewServer(addr, h, a.bus)

	// Start cron scheduler in background.
	go a.scheduler.Start(ctx)

	if err := srv.Start(ctx); err != nil {
		slog.Error("server stopped", "err", err)
	}
}

func runChat(ctx context.Context, a *app) {
	fmt.Println("kagent chat mode — type your task and press Enter. Ctrl+C to exit.")
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
	if os.Getenv("KAGENT_DEBUG") != "" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}

func loadConfig() {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./configs")
	viper.AddConfigPath("$HOME/.kagent")
	viper.AddConfigPath(".")

	viper.SetDefault("llm.provider", "openai")
	viper.SetDefault("llm.model", "gpt-4o")
	viper.SetDefault("max_steps", 30)
	viper.SetDefault("server.addr", ":8080")

	viper.AutomaticEnv()
	viper.SetEnvPrefix("KAGENT")

	if err := viper.ReadInConfig(); err != nil {
		// Config file is optional.
		slog.Debug("no config file found, using defaults and env vars")
	}
}
