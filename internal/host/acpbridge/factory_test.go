package acpbridge

import (
	"context"
	"testing"

	"github.com/inhere/axon/internal/host/session"
)

func TestNewRunner_ReturnsNilWhenModeEmpty(t *testing.T) {
	t.Parallel()

	runner, err := NewRunner(FactoryConfig{})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if runner != nil {
		t.Fatalf("NewRunner() = %T, want nil", runner)
	}
}

func TestNewRunner_StubModeBuildsRunner(t *testing.T) {
	t.Parallel()

	runner, err := NewRunner(FactoryConfig{
		Mode:                "stub",
		AdapterName:         "codex",
		Workspace:           "workspace/demo",
		StubRemoteAgentName: "codex-cli",
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), session.RuntimeRequest{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "implement feature x",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExternalTask == nil {
		t.Fatal("Run().ExternalTask is nil")
	}
	if result.ExternalTask.RemoteAgentName != "codex-cli" {
		t.Fatalf("Run().ExternalTask.RemoteAgentName = %q, want %q", result.ExternalTask.RemoteAgentName, "codex-cli")
	}
}

func TestNewRunner_LocalCommandModeBuildsRunner(t *testing.T) {
	t.Parallel()

	runner, err := NewRunner(FactoryConfig{
		Mode:        "local_command",
		AdapterName: "codex",
		Workspace:   "workspace/demo",
		LocalCommand: CommandConfig{
			Command: []string{"codex", "exec"},
		},
		CommandExecutor: func(_ context.Context, _ CommandSpec) (CommandOutput, error) {
			return CommandOutput{
				Stdout: `{"state":"completed","summary":"done by local command","remote_agent_name":"codex-cli"}`,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), session.RuntimeRequest{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "implement feature x",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Summary != "done by local command" {
		t.Fatalf("Run().Summary = %q, want %q", result.Summary, "done by local command")
	}
}
