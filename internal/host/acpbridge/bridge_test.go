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
			Metadata: map[string]any{
				"protocol": "codex_app_server",
				"items": []any{
					map[string]any{
						"itemId": "item_001",
						"type":   "agent_message",
					},
				},
			},
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
		Context: map[string]any{
			"external_task_id": "ext_001",
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Summary != "external work done" {
		t.Fatalf("Run().Summary = %q, want %q", result.Summary, "external work done")
	}
	if result.ExternalTask == nil {
		t.Fatal("Run().ExternalTask is nil")
	}
	if result.ExternalTask.ExternalTaskID != "ext_001" {
		t.Fatalf("Run().ExternalTask.ExternalTaskID = %q, want %q", result.ExternalTask.ExternalTaskID, "ext_001")
	}
	if result.ExternalTask.Metadata["workspace"] != "workspace/demo" {
		t.Fatalf("Run().ExternalTask.Metadata[workspace] = %v, want %q", result.ExternalTask.Metadata["workspace"], "workspace/demo")
	}
	if result.ExternalTask.Metadata["protocol"] != "codex_app_server" {
		t.Fatalf("Run().ExternalTask.Metadata[protocol] = %v, want %q", result.ExternalTask.Metadata["protocol"], "codex_app_server")
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

func TestRunnerAdapter_RunReturnsExternalTaskForFailedDispatch(t *testing.T) {
	t.Parallel()

	fakeBridge := &fakeBridge{
		result: DispatchResult{
			State:           StateFailed,
			Error:           "external failed",
			RemoteAgentName: "codex-cli",
			RemoteSessionID: "thread_001",
			RemoteTaskID:    "turn_remote_001",
			Metadata: map[string]any{
				"protocol": "codex_app_server",
			},
		},
	}
	adapter := NewRunnerAdapter(fakeBridge, RunnerAdapterConfig{
		AdapterName: "claude-code",
		Workspace:   "workspace/demo",
	})

	result, err := adapter.Run(context.Background(), session.RuntimeRequest{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "implement broken feature",
		Context: map[string]any{
			"external_task_id": "ext_001",
		},
	})
	if err == nil {
		t.Fatal("Run() error = nil, want failed dispatch error")
	}
	if result.ExternalTask == nil {
		t.Fatal("Run().ExternalTask is nil")
	}
	if result.ExternalTask.ExternalTaskID != "ext_001" {
		t.Fatalf("Run().ExternalTask.ExternalTaskID = %q, want %q", result.ExternalTask.ExternalTaskID, "ext_001")
	}
	if result.ExternalTask.RemoteSessionID != "thread_001" {
		t.Fatalf("Run().ExternalTask.RemoteSessionID = %q, want %q", result.ExternalTask.RemoteSessionID, "thread_001")
	}
	if result.ExternalTask.RemoteTaskID != "turn_remote_001" {
		t.Fatalf("Run().ExternalTask.RemoteTaskID = %q, want %q", result.ExternalTask.RemoteTaskID, "turn_remote_001")
	}
	if result.ExternalTask.State != string(StateFailed) {
		t.Fatalf("Run().ExternalTask.State = %q, want %q", result.ExternalTask.State, StateFailed)
	}
	if result.ExternalTask.Metadata["protocol"] != "codex_app_server" {
		t.Fatalf("Run().ExternalTask.Metadata[protocol] = %v, want %q", result.ExternalTask.Metadata["protocol"], "codex_app_server")
	}
}

func TestRunnerAdapter_RunPassesResumeRemoteSessionToBridge(t *testing.T) {
	t.Parallel()

	fakeBridge := &fakeBridge{
		result: DispatchResult{
			State:   StateCompleted,
			Summary: "continued",
		},
	}
	adapter := NewRunnerAdapter(fakeBridge, RunnerAdapterConfig{
		AdapterName: "codex",
		Workspace:   "workspace/demo",
	})

	_, err := adapter.Run(context.Background(), session.RuntimeRequest{
		SessionID:   "sess_001",
		TurnID:      "turn_002",
		RunID:       "run_002",
		AgentID:     "default",
		Instruction: "continue task",
		Context: map[string]any{
			"external_task_id": "ext_002",
			"resume_external_tasks": []session.ExternalTask{
				{
					Adapter:         "claude-code",
					RemoteSessionID: "claude_thread_001",
				},
				{
					Adapter:         "codex",
					RemoteSessionID: "thread_001",
					RemoteTaskID:    "turn_remote_001",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if fakeBridge.lastRequest.Context["resume_remote_session_id"] != "thread_001" {
		t.Fatalf("Dispatch().Context[resume_remote_session_id] = %v, want %q", fakeBridge.lastRequest.Context["resume_remote_session_id"], "thread_001")
	}
	if fakeBridge.lastRequest.Context["resume_remote_task_id"] != "turn_remote_001" {
		t.Fatalf("Dispatch().Context[resume_remote_task_id] = %v, want %q", fakeBridge.lastRequest.Context["resume_remote_task_id"], "turn_remote_001")
	}
	if fakeBridge.lastRequest.Context["external_task_id"] == nil {
		t.Fatal("Dispatch().Context[external_task_id] is nil")
	}
}

func TestRunnerAdapter_RunMapsDispatchEventsToRuntimeEvents(t *testing.T) {
	t.Parallel()

	fakeBridge := &fakeBridge{
		result: DispatchResult{
			State:   StateCompleted,
			Summary: "continued",
		},
	}
	fakeBridge.dispatch = func(req DispatchRequest) {
		if req.Emit == nil {
			return
		}
		_ = req.Emit(DispatchEvent{
			Type: "external.activity",
			Payload: map[string]any{
				"kind": "item_started",
			},
		})
		_ = req.Emit(DispatchEvent{
			Type: "external.message.delta",
			Payload: map[string]any{
				"messageId":    "turn_remote_001:item_001",
				"delta":        "hello",
				"isFirstChunk": true,
			},
		})
		_ = req.Emit(DispatchEvent{
			Type: "external.message.end",
			Payload: map[string]any{
				"messageId": "turn_remote_001:item_001",
			},
		})
	}

	adapter := NewRunnerAdapter(fakeBridge, RunnerAdapterConfig{
		AdapterName: "codex",
		Workspace:   "workspace/demo",
	})

	var runtimeEvents []session.RuntimeEvent
	_, err := adapter.Run(context.Background(), session.RuntimeRequest{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "continue task",
		Context: map[string]any{
			"external_task_id": "ext_001",
		},
		Emit: func(event session.RuntimeEvent) error {
			runtimeEvents = append(runtimeEvents, event)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(runtimeEvents) != 5 {
		t.Fatalf("runtime events len = %d, want 5", len(runtimeEvents))
	}
	if runtimeEvents[0].EventType != session.EventRunStatus {
		t.Fatalf("runtime events[0].EventType = %q, want %q", runtimeEvents[0].EventType, session.EventRunStatus)
	}
	if runtimeEvents[0].Payload["state"] != string(session.RunDispatching) {
		t.Fatalf("runtime events[0].Payload[state] = %v, want %q", runtimeEvents[0].Payload["state"], session.RunDispatching)
	}
	if runtimeEvents[1].EventType != session.EventRunStatus {
		t.Fatalf("runtime events[1].EventType = %q, want %q", runtimeEvents[1].EventType, session.EventRunStatus)
	}
	if runtimeEvents[1].Payload["state"] != string(session.RunExternal) {
		t.Fatalf("runtime events[1].Payload[state] = %v, want %q", runtimeEvents[1].Payload["state"], session.RunExternal)
	}
	waitMetadata, ok := runtimeEvents[1].Payload["waitMetadata"].(map[string]any)
	if !ok {
		t.Fatalf("runtime events[1].Payload[waitMetadata] type = %T, want map[string]any", runtimeEvents[1].Payload["waitMetadata"])
	}
	if waitMetadata["wait_kind"] != "external_running" {
		t.Fatalf("runtime events[1].Payload[waitMetadata][wait_kind] = %v, want %q", waitMetadata["wait_kind"], "external_running")
	}
	if waitMetadata["activity_kind"] != "item_started" {
		t.Fatalf("runtime events[1].Payload[waitMetadata][activity_kind] = %v, want %q", waitMetadata["activity_kind"], "item_started")
	}
	if runtimeEvents[2].EventType != session.EventRunExternalActivity {
		t.Fatalf("runtime events[2].EventType = %q, want %q", runtimeEvents[2].EventType, session.EventRunExternalActivity)
	}
	if runtimeEvents[2].Payload["externalTaskId"] != "ext_001" {
		t.Fatalf("runtime events[2].Payload[externalTaskId] = %v, want %q", runtimeEvents[2].Payload["externalTaskId"], "ext_001")
	}
	if runtimeEvents[3].EventType != session.EventRunExternalMessageDelta {
		t.Fatalf("runtime events[3].EventType = %q, want %q", runtimeEvents[3].EventType, session.EventRunExternalMessageDelta)
	}
	if runtimeEvents[3].Payload["externalTaskId"] != "ext_001" {
		t.Fatalf("runtime events[3].Payload[externalTaskId] = %v, want %q", runtimeEvents[3].Payload["externalTaskId"], "ext_001")
	}
	if runtimeEvents[4].EventType != session.EventRunExternalMessageEnd {
		t.Fatalf("runtime events[4].EventType = %q, want %q", runtimeEvents[4].EventType, session.EventRunExternalMessageEnd)
	}
	if runtimeEvents[4].Payload["externalTaskId"] != "ext_001" {
		t.Fatalf("runtime events[4].Payload[externalTaskId] = %v, want %q", runtimeEvents[4].Payload["externalTaskId"], "ext_001")
	}
}

func TestRunnerAdapter_RunEmitsDispatchingAndExternalRunningStatus(t *testing.T) {
	t.Parallel()

	fakeBridge := &fakeBridge{
		result: DispatchResult{
			State:   StateCompleted,
			Summary: "continued",
		},
	}
	fakeBridge.dispatch = func(req DispatchRequest) {
		if req.Emit == nil {
			return
		}
		_ = req.Emit(DispatchEvent{
			Type: "external.activity",
			Payload: map[string]any{
				"kind": "item_started",
			},
		})
	}

	adapter := NewRunnerAdapter(fakeBridge, RunnerAdapterConfig{
		AdapterName: "codex",
		Workspace:   "workspace/demo",
	})

	var runtimeEvents []session.RuntimeEvent
	_, err := adapter.Run(context.Background(), session.RuntimeRequest{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "continue task",
		Emit: func(event session.RuntimeEvent) error {
			runtimeEvents = append(runtimeEvents, event)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(runtimeEvents) != 3 {
		t.Fatalf("runtime events len = %d, want 3", len(runtimeEvents))
	}
	if runtimeEvents[0].EventType != session.EventRunStatus {
		t.Fatalf("runtime events[0].EventType = %q, want %q", runtimeEvents[0].EventType, session.EventRunStatus)
	}
	if runtimeEvents[0].Payload["state"] != string(session.RunDispatching) {
		t.Fatalf("runtime events[0].Payload[state] = %v, want %q", runtimeEvents[0].Payload["state"], session.RunDispatching)
	}
	if runtimeEvents[1].EventType != session.EventRunStatus {
		t.Fatalf("runtime events[1].EventType = %q, want %q", runtimeEvents[1].EventType, session.EventRunStatus)
	}
	if runtimeEvents[1].Payload["state"] != string(session.RunExternal) {
		t.Fatalf("runtime events[1].Payload[state] = %v, want %q", runtimeEvents[1].Payload["state"], session.RunExternal)
	}
	waitMetadata, ok := runtimeEvents[1].Payload["waitMetadata"].(map[string]any)
	if !ok {
		t.Fatalf("runtime events[1].Payload[waitMetadata] type = %T, want map[string]any", runtimeEvents[1].Payload["waitMetadata"])
	}
	if waitMetadata["wait_kind"] != "external_running" {
		t.Fatalf("runtime events[1].Payload[waitMetadata][wait_kind] = %v, want %q", waitMetadata["wait_kind"], "external_running")
	}
	if runtimeEvents[2].EventType != session.EventRunExternalActivity {
		t.Fatalf("runtime events[2].EventType = %q, want %q", runtimeEvents[2].EventType, session.EventRunExternalActivity)
	}
}

func TestRunnerAdapter_RunReturnsExternalWaitingStateForRunningDispatch(t *testing.T) {
	t.Parallel()

	fakeBridge := &fakeBridge{
		result: DispatchResult{
			State:           StateRunning,
			Summary:         "still running remotely",
			RemoteAgentName: "codex-cli",
			RemoteSessionID: "thread_001",
			RemoteTaskID:    "turn_remote_001",
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
		Instruction: "continue task",
		Context: map[string]any{
			"external_task_id": "ext_001",
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.WaitState != session.RunExternal {
		t.Fatalf("Run().WaitState = %q, want %q", result.WaitState, session.RunExternal)
	}
	if result.WaitMetadata["wait_kind"] != "external_running" {
		t.Fatalf("Run().WaitMetadata[wait_kind] = %v, want %q", result.WaitMetadata["wait_kind"], "external_running")
	}
	if result.WaitMetadata["remote_session_id"] != "thread_001" {
		t.Fatalf("Run().WaitMetadata[remote_session_id] = %v, want %q", result.WaitMetadata["remote_session_id"], "thread_001")
	}
	if result.ExternalTask == nil {
		t.Fatal("Run().ExternalTask is nil")
	}
	if result.ExternalTask.State != string(StateRunning) {
		t.Fatalf("Run().ExternalTask.State = %q, want %q", result.ExternalTask.State, StateRunning)
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
	dispatch    func(req DispatchRequest)
}

func (f *fakeBridge) Dispatch(_ context.Context, req DispatchRequest) (DispatchResult, error) {
	f.lastRequest = req
	if f.dispatch != nil {
		f.dispatch(req)
	}
	return f.result, f.err
}
