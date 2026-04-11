package acpbridge

import (
	"fmt"
	"strings"

	"github.com/inhere/axon/internal/host/session"
)

type FactoryConfig struct {
	Mode                string
	AdapterName         string
	Workspace           string
	StubRemoteAgentName string
	LocalCommand        CommandConfig
	ACPCommand          CommandConfig
	CommandExecutor     CommandExecutor
	ACPStdioStarter     ACPStdioSessionStarter
}

func NewRunner(cfg FactoryConfig) (session.Runner, error) {
	mode := strings.TrimSpace(cfg.Mode)
	if mode == "" || mode == "none" {
		return nil, nil
	}

	runnerCfg := RunnerAdapterConfig{
		AdapterName: cfg.AdapterName,
		Workspace:   cfg.Workspace,
	}

	switch mode {
	case "stub":
		return NewRunnerAdapter(NewStubBridge(StubBridgeConfig{
			RemoteAgentName: cfg.StubRemoteAgentName,
		}), runnerCfg), nil
	case "local_command":
		if len(cfg.LocalCommand.Command) == 0 {
			return nil, fmt.Errorf("local command config is required for local_command mode")
		}
		return NewRunnerAdapter(NewLocalCommandBridge(LocalCommandBridgeConfig{
			Commands: map[string]CommandConfig{
				strings.TrimSpace(cfg.AdapterName): cfg.LocalCommand,
			},
			Executor: cfg.CommandExecutor,
		}), runnerCfg), nil
	case "acp_stdio":
		acpCommand := cfg.ACPCommand
		if len(acpCommand.Command) == 0 {
			return nil, fmt.Errorf("acp command config is required for acp_stdio mode")
		}
		return NewRunnerAdapter(NewACPStdioBridge(ACPStdioBridgeConfig{
			Commands: map[string]CommandConfig{
				strings.TrimSpace(cfg.AdapterName): acpCommand,
			},
			SessionStarter: cfg.ACPStdioStarter,
		}), runnerCfg), nil
	case "codex_app_server":
		codexCommand := cfg.ACPCommand
		if len(codexCommand.Command) == 0 {
			codexCommand = defaultCodexAppServerCommand()
		}
		return NewRunnerAdapter(NewCodexAppServerBridge(CodexAppServerBridgeConfig{
			Commands: map[string]CommandConfig{
				strings.TrimSpace(cfg.AdapterName): codexCommand,
			},
			SessionStarter: cfg.ACPStdioStarter,
		}), runnerCfg), nil
	default:
		return nil, fmt.Errorf("unsupported acp bridge mode %q", mode)
	}
}

// NewLocalCommandTestRunner is a small helper kept in-package for tests and
// future wiring experiments that need an injected executor without touching the
// main server factory shape.
func NewLocalCommandTestRunner(adapterName, workspace string, cmdCfg CommandConfig, executor CommandExecutor) session.Runner {
	runner, _ := NewRunner(FactoryConfig{
		Mode:            "local_command",
		AdapterName:     adapterName,
		Workspace:       workspace,
		LocalCommand:    cmdCfg,
		CommandExecutor: executor,
	})
	return runner
}

func defaultCodexAppServerCommand() CommandConfig {
	return CommandConfig{
		Command: []string{
			"codex",
			"app-server",
			"-c",
			`model_reasoning_summary="detailed"`,
		},
	}
}
