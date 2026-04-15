package app

import (
	"context"
	"log/slog"
	"strings"

	gkconfig "github.com/gookit/config/v2"

	appconfig "github.com/inhere/axon/internal/config"
	"github.com/inhere/axon/internal/host/acpbridge"
	"github.com/inhere/axon/internal/host/gateway"
	"github.com/inhere/axon/internal/host/intrunner"
	"github.com/inhere/axon/internal/host/session"
)

func (a *App) RunServer(ctx context.Context) error {
	sessionStore, err := session.NewSQLiteStore(appconfig.ResolveSessionDBPath())
	if err != nil {
		slog.Error("failed to initialize session store", "err", err)
		return err
	}
	defer func() {
		if err := sessionStore.Close(); err != nil {
			slog.Warn("failed to close session store", "err", err)
		}
	}()

	sessionRunner, err := a.buildSessionRunner()
	if err != nil {
		slog.Error("failed to build session runner", "err", err)
		return err
	}

	sessionSvc := session.NewService(sessionStore, a.bus, sessionRunner, session.ServiceConfig{})
	h := gateway.NewHandler(a.orch, sessionStore, sessionSvc, a.bus, a.hitlMgr)
	addr := gkconfig.String("server.addr", ":8180")
	if addr == "" {
		addr = ":8180"
	}
	srv := gateway.NewServer(addr, h, a.bus)

	go a.scheduler.Start(ctx)

	if err := srv.Start(ctx); err != nil {
		slog.Error("server stopped", "err", err)
		return err
	}
	return nil
}

func (a *App) buildSessionRunner() (session.Runner, error) {
	mode := strings.TrimSpace(gkconfig.String("host.acp.mode"))
	if mode == "" {
		return intrunner.New(intrunner.Config{
			Provider:      a.provider,
			Registry:      a.registry,
			Tracer:        a.tracer,
			HITL:          a.hitlMgr,
			RunRoot:       appconfig.ResolveHostAgentcoreRunRoot(),
			Runtime:       gkconfig.String("agent.runtime"),
			FailThreshold: gkconfig.Int("hitl.fail_threshold", 3),
			MaxSteps:      gkconfig.Int("max_steps", 10),
		})
	}

	adapterName := strings.TrimSpace(gkconfig.String("host.acp.adapter"))
	if adapterName == "" {
		adapterName = "codex"
	}

	localCommand := strings.Fields(strings.TrimSpace(gkconfig.String("host.acp.local_command")))
	acpCommand := strings.Fields(strings.TrimSpace(gkconfig.String("host.acp.acp_command")))
	if mode == "codex_app_server" && len(acpCommand) == 0 {
		slog.Info("host acp acp_command not configured, using codex app-server default")
	}

	return acpbridge.NewRunner(acpbridge.FactoryConfig{
		Mode:                mode,
		AdapterName:         adapterName,
		Workspace:           appconfig.ResolveWorkspacePath(),
		StubRemoteAgentName: strings.TrimSpace(gkconfig.String("host.acp.stub_remote_agent_name")),
		LocalCommand: acpbridge.CommandConfig{
			Command: localCommand,
		},
		ACPCommand: acpbridge.CommandConfig{
			Command: acpCommand,
		},
	})
}
