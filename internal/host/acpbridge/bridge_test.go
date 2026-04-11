package acpbridge

import (
	"context"
	"errors"
	"testing"

	"github.com/inhere/axon/internal/host/session"
)

func TestRunnerAdapter_RunDispatchesTaskToBridge(t *testing.T) {
	t.Parallel()

	fakeBridge := &fakeBridge{
		result: DispatchResult{
			State:   StateCompleted,
			Summary: "external work done",
		},
	}
	adapter := NewRunnerAdapter(fakeBridge, RunnerAdapterConfig{
		AdapterName: "codex",
		Workspace:   "workspace/demo",
	})

	result, err := adapter.Run(context.Background(), session.RuntimeRequest{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "implement feature x",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Summary != "external work done" {
		t.Fatalf("Run().Summary = %q, want %q", result.Summary, "external work done")
	}
	if fakeBridge.lastRequest.RunID != "run_001" {
		t.Fatalf("Dispatch().RunID = %q, want %q", fakeBridge.lastRequest.RunID, "run_001")
	}
	if fakeBridge.lastRequest.Workspace != "workspace/demo" {
		t.Fatalf("Dispatch().Workspace = %q, want %q", fakeBridge.lastRequest.Workspace, "workspace/demo")
	}
	if fakeBridge.lastRequest.Adapter != "codex" {
		t.Fatalf("Dispatch().Adapter = %q, want %q", fakeBridge.lastRequest.Adapter, "codex")
	}
}

func TestRunnerAdapter_RunReturnsErrorForFailedDispatch(t *testing.T) {
	t.Parallel()

	fakeBridge := &fakeBridge{
		result: DispatchResult{
			State: StateFailed,
			Error: "external failed",
		},
	}
	adapter := NewRunnerAdapter(fakeBridge, RunnerAdapterConfig{
		AdapterName: "claude-code",
		Workspace:   "workspace/demo",
	})

	_, err := adapter.Run(context.Background(), session.RuntimeRequest{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "implement broken feature",
	})
	if err == nil {
		t.Fatal("Run() error = nil, want failed dispatch error")
	}
}

func TestLocalCommandBridge_DispatchParsesJSONResult(t *testing.T) {
	t.Parallel()

	var gotSpec CommandSpec
	bridge := NewLocalCommandBridge(LocalCommandBridgeConfig{
		Commands: map[string]CommandConfig{
			"codex": {
				Command: []string{"codex", "exec"},
			},
		},
		Executor: func(_ context.Context, spec CommandSpec) (CommandOutput, error) {
			gotSpec = spec
			return CommandOutput{
				Stdout: `{"state":"completed","summary":"done by local command","remote_agent_name":"codex-cli","remote_session_id":"remote_sess_001","remote_task_id":"remote_task_001","artifacts":["artifact://local/result"]}`,
			}, nil
		},
	})

	result, err := bridge.Dispatch(context.Background(), DispatchRequest{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Adapter:     "codex",
		Instruction: "implement feature x",
		Workspace:   "workspace/demo",
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if gotSpec.Args[0] != "codex" {
		t.Fatalf("executor args[0] = %q, want %q", gotSpec.Args[0], "codex")
	}
	if gotSpec.Workdir != "workspace/demo" {
		t.Fatalf("executor workdir = %q, want %q", gotSpec.Workdir, "workspace/demo")
	}
	if gotSpec.Env["AXON_SESSION_ID"] != "sess_001" {
		t.Fatalf("executor env AXON_SESSION_ID = %q, want %q", gotSpec.Env["AXON_SESSION_ID"], "sess_001")
	}
	if result.State != StateCompleted {
		t.Fatalf("Dispatch().State = %q, want %q", result.State, StateCompleted)
	}
	if result.RemoteTaskID != "remote_task_001" {
		t.Fatalf("Dispatch().RemoteTaskID = %q, want %q", result.RemoteTaskID, "remote_task_001")
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("Dispatch().Artifacts len = %d, want 1", len(result.Artifacts))
	}
}

func TestLocalCommandBridge_DispatchFallsBackToStdoutSummary(t *testing.T) {
	t.Parallel()

	bridge := NewLocalCommandBridge(LocalCommandBridgeConfig{
		Commands: map[string]CommandConfig{
			"claude-code": {
				Command: []string{"claude-code"},
			},
		},
		Executor: func(_ context.Context, _ CommandSpec) (CommandOutput, error) {
			return CommandOutput{
				Stdout: "plain stdout result",
			}, nil
		},
	})

	result, err := bridge.Dispatch(context.Background(), DispatchRequest{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Adapter:     "claude-code",
		Instruction: "continue task",
		Workspace:   "workspace/demo",
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result.Summary != "plain stdout result" {
		t.Fatalf("Dispatch().Summary = %q, want %q", result.Summary, "plain stdout result")
	}
	if result.State != StateCompleted {
		t.Fatalf("Dispatch().State = %q, want %q", result.State, StateCompleted)
	}
}

func TestLocalCommandBridge_DispatchReturnsExecutorError(t *testing.T) {
	t.Parallel()

	bridge := NewLocalCommandBridge(LocalCommandBridgeConfig{
		Commands: map[string]CommandConfig{
			"codex": {
				Command: []string{"codex"},
			},
		},
		Executor: func(_ context.Context, _ CommandSpec) (CommandOutput, error) {
			return CommandOutput{}, errors.New("exec failed")
		},
	})

	_, err := bridge.Dispatch(context.Background(), DispatchRequest{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Adapter:     "codex",
		Instruction: "continue task",
		Workspace:   "workspace/demo",
	})
	if err == nil {
		t.Fatal("Dispatch() error = nil, want executor failure")
	}
}

type fakeBridge struct {
	lastRequest DispatchRequest
	result      DispatchResult
	err         error
}

func (f *fakeBridge) Dispatch(_ context.Context, req DispatchRequest) (DispatchResult, error) {
	f.lastRequest = req
	return f.result, f.err
}
