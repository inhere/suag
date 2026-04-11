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

func TestNewRunner_ACPStdioModeBuildsRunner(t *testing.T) {
	t.Parallel()

	fake := &fakeACPStdioSession{
		readLines: [][]byte{
			[]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","id":2,"result":{"sessionId":"remote_sess_001"}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"remote_sess_001","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"acp result"}}}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn","userMessageId":"msg_001"}}` + "\n"),
		},
	}

	runner, err := NewRunner(FactoryConfig{
		Mode:        "acp_stdio",
		AdapterName: "codex",
		Workspace:   "workspace/demo",
		ACPCommand: CommandConfig{
			Command: []string{"codex", "acp"},
		},
		ACPStdioStarter: func(_ context.Context, _ CommandSpec) (ACPStdioSession, error) {
			return fake, nil
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
	if result.Summary != "acp result" {
		t.Fatalf("Run().Summary = %q, want %q", result.Summary, "acp result")
	}
	if result.ExternalTask == nil {
		t.Fatal("Run().ExternalTask is nil")
	}
	if result.ExternalTask.RemoteSessionID != "remote_sess_001" {
		t.Fatalf("Run().ExternalTask.RemoteSessionID = %q, want %q", result.ExternalTask.RemoteSessionID, "remote_sess_001")
	}
}

func TestNewRunner_ACPStdioModeRequiresCommandWhenOmitted(t *testing.T) {
	t.Parallel()

	runner, err := NewRunner(FactoryConfig{
		Mode:        "acp_stdio",
		AdapterName: "codex",
		Workspace:   "workspace/demo",
	})
	if err == nil {
		t.Fatal("NewRunner() error = nil, want missing acp command error")
	}
	if runner != nil {
		t.Fatalf("NewRunner() = %T, want nil", runner)
	}
}

func TestNewRunner_CodexAppServerModeUsesDefaultCommandWhenOmitted(t *testing.T) {
	t.Parallel()

	fake := &fakeACPStdioSession{
		readLines: [][]byte{
			[]byte(`{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"codex","version":"0.1.0"}}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","id":2,"result":{"threadId":"thread_001"}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","id":3,"result":{"turnId":"turn_remote_001"}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thread_001","turnId":"turn_remote_001"}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","method":"item/agentMessage/delta","params":{"threadId":"thread_001","turnId":"turn_remote_001","itemId":"item_001","delta":"preset result"}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thread_001","turnId":"turn_remote_001","stopReason":"end_turn"}}` + "\n"),
		},
	}

	var gotSpec CommandSpec
	runner, err := NewRunner(FactoryConfig{
		Mode:        "codex_app_server",
		AdapterName: "codex",
		Workspace:   "workspace/demo",
		ACPStdioStarter: func(_ context.Context, spec CommandSpec) (ACPStdioSession, error) {
			gotSpec = spec
			return fake, nil
		},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, err = runner.Run(context.Background(), session.RuntimeRequest{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "implement feature x",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(fake.writtenLines) != 4 {
		t.Fatalf("written lines len = %d, want 4", len(fake.writtenLines))
	}
	if gotSpec.Args[0] != "codex" || gotSpec.Args[1] != "app-server" || gotSpec.Args[2] != "-c" {
		t.Fatalf("starter args prefix = %v, want [codex app-server -c]", gotSpec.Args)
	}
	if len(gotSpec.Args) != 4 {
		t.Fatalf("starter args len = %d, want 4", len(gotSpec.Args))
	}
	if gotSpec.Args[3] != `model_reasoning_summary="detailed"` {
		t.Fatalf("starter args[3] = %q, want %q", gotSpec.Args[3], `model_reasoning_summary="detailed"`)
	}
}
