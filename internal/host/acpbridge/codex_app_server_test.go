package acpbridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestCodexAppServerBridge_DispatchRunsCodexProtocol(t *testing.T) {
	t.Parallel()

	fake := &fakeACPStdioSession{
		readLines: [][]byte{
			[]byte(`{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"codex","version":"0.1.0"}}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","id":2,"result":{"threadId":"thread_001"}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","id":3,"result":{"turnId":"turn_remote_001"}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thread_001","turnId":"turn_remote_001"}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","method":"item/started","params":{"threadId":"thread_001","turnId":"turn_remote_001","itemId":"item_cmd_001","itemType":"commandExecution","item":{"id":"item_cmd_001","type":"commandExecution","command":"go test ./...","cwd":"workspace/demo","status":"in_progress"}}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","method":"item/commandExecution/outputDelta","params":{"threadId":"thread_001","turnId":"turn_remote_001","itemId":"item_cmd_001","delta":"ok github.com/inhere/axon/internal/host/acpbridge"}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thread_001","turnId":"turn_remote_001","itemId":"item_cmd_001","itemType":"commandExecution","item":{"id":"item_cmd_001","type":"commandExecution","command":"go test ./...","cwd":"workspace/demo","status":"completed","aggregatedOutput":"ok github.com/inhere/axon/internal/host/acpbridge","exitCode":0}}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","method":"item/agentMessage/delta","params":{"threadId":"thread_001","turnId":"turn_remote_001","itemId":"item_001","delta":"hello "}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","method":"item/agentMessage/delta","params":{"threadId":"thread_001","turnId":"turn_remote_001","itemId":"item_001","delta":"codex"}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thread_001","turnId":"turn_remote_001","stopReason":"end_turn"}}` + "\n"),
		},
	}

	var gotSpec CommandSpec
	bridge := NewCodexAppServerBridge(CodexAppServerBridgeConfig{
		Commands: map[string]CommandConfig{
			"codex": {
				Command: []string{"codex", "app-server"},
			},
		},
		SessionStarter: func(_ context.Context, spec CommandSpec) (ACPStdioSession, error) {
			gotSpec = spec
			return fake, nil
		},
	})

	result, err := bridge.Dispatch(context.Background(), DispatchRequest{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Adapter:     "codex",
		Instruction: "analyze project",
		Workspace:   "workspace/demo",
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if gotSpec.Args[0] != "codex" {
		t.Fatalf("starter args[0] = %q, want %q", gotSpec.Args[0], "codex")
	}
	if gotSpec.Workdir != "workspace/demo" {
		t.Fatalf("starter workdir = %q, want %q", gotSpec.Workdir, "workspace/demo")
	}
	if gotSpec.Env["AXON_INSTRUCTION"] != "analyze project" {
		t.Fatalf("starter env AXON_INSTRUCTION = %q, want %q", gotSpec.Env["AXON_INSTRUCTION"], "analyze project")
	}
	if result.State != StateCompleted {
		t.Fatalf("Dispatch().State = %q, want %q", result.State, StateCompleted)
	}
	if result.Summary != "hello codex" {
		t.Fatalf("Dispatch().Summary = %q, want %q", result.Summary, "hello codex")
	}
	if result.RemoteSessionID != "thread_001" {
		t.Fatalf("Dispatch().RemoteSessionID = %q, want %q", result.RemoteSessionID, "thread_001")
	}
	if result.RemoteTaskID != "turn_remote_001" {
		t.Fatalf("Dispatch().RemoteTaskID = %q, want %q", result.RemoteTaskID, "turn_remote_001")
	}
	if result.Metadata["protocol"] != "codex_app_server" {
		t.Fatalf("Dispatch().Metadata[protocol] = %v, want %q", result.Metadata["protocol"], "codex_app_server")
	}
	if result.Metadata["stopReason"] != "end_turn" {
		t.Fatalf("Dispatch().Metadata[stopReason] = %v, want %q", result.Metadata["stopReason"], "end_turn")
	}
	items, ok := result.Metadata["items"].([]map[string]any)
	if !ok {
		t.Fatalf("Dispatch().Metadata[items] type = %T, want []map[string]any", result.Metadata["items"])
	}
	if len(items) != 1 {
		t.Fatalf("Dispatch().Metadata[items] len = %d, want 1", len(items))
	}
	if items[0]["command"] != "go test ./..." {
		t.Fatalf("Dispatch().Metadata[items][0][command] = %v, want %q", items[0]["command"], "go test ./...")
	}
	if items[0]["outputPreview"] != "ok github.com/inhere/axon/internal/host/acpbridge" {
		t.Fatalf("Dispatch().Metadata[items][0][outputPreview] = %v, want preview text", items[0]["outputPreview"])
	}
	eventSummary, ok := result.Metadata["eventSummary"].(map[string]any)
	if !ok {
		t.Fatalf("Dispatch().Metadata[eventSummary] type = %T, want map[string]any", result.Metadata["eventSummary"])
	}
	if eventSummary["commandDeltaCount"] != 1 {
		t.Fatalf("Dispatch().Metadata[eventSummary][commandDeltaCount] = %v, want %d", eventSummary["commandDeltaCount"], 1)
	}
	if eventSummary["messageDeltaCount"] != 2 {
		t.Fatalf("Dispatch().Metadata[eventSummary][messageDeltaCount] = %v, want %d", eventSummary["messageDeltaCount"], 2)
	}

	if len(fake.writtenLines) != 4 {
		t.Fatalf("written lines len = %d, want 4", len(fake.writtenLines))
	}

	var initReq map[string]any
	if err := json.Unmarshal(fake.writtenLines[0], &initReq); err != nil {
		t.Fatalf("json.Unmarshal() initialize request error = %v", err)
	}
	if initReq["method"] != "initialize" {
		t.Fatalf("initialize request method = %v, want initialize", initReq["method"])
	}

	var initializedReq map[string]any
	if err := json.Unmarshal(fake.writtenLines[1], &initializedReq); err != nil {
		t.Fatalf("json.Unmarshal() initialized notification error = %v", err)
	}
	if initializedReq["method"] != "initialized" {
		t.Fatalf("initialized notification method = %v, want initialized", initializedReq["method"])
	}

	var threadReq map[string]any
	if err := json.Unmarshal(fake.writtenLines[2], &threadReq); err != nil {
		t.Fatalf("json.Unmarshal() thread/start request error = %v", err)
	}
	if threadReq["method"] != "thread/start" {
		t.Fatalf("thread/start request method = %v, want thread/start", threadReq["method"])
	}

	var turnReq map[string]any
	if err := json.Unmarshal(fake.writtenLines[3], &turnReq); err != nil {
		t.Fatalf("json.Unmarshal() turn/start request error = %v", err)
	}
	if turnReq["method"] != "turn/start" {
		t.Fatalf("turn/start request method = %v, want turn/start", turnReq["method"])
	}
}

func TestCodexAppServerBridge_DispatchReturnsFailedStateOnTurnError(t *testing.T) {
	t.Parallel()

	fake := &fakeACPStdioSession{
		readLines: [][]byte{
			[]byte(`{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"codex","version":"0.1.0"}}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thread_001"}}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","id":3,"result":{"turn":{"id":"turn_remote_001"}}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","method":"error","params":{"threadId":"thread_001","turnId":"turn_remote_001","error":{"message":"permission denied"},"willRetry":false}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thread_001","turn":{"id":"turn_remote_001","status":"failed","error":{"message":"permission denied"}}}}` + "\n"),
		},
	}

	bridge := NewCodexAppServerBridge(CodexAppServerBridgeConfig{
		Commands: map[string]CommandConfig{
			"codex": {
				Command: []string{"codex", "app-server"},
			},
		},
		SessionStarter: func(_ context.Context, _ CommandSpec) (ACPStdioSession, error) {
			return fake, nil
		},
	})

	result, err := bridge.Dispatch(context.Background(), DispatchRequest{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Adapter:     "codex",
		Instruction: "analyze project",
		Workspace:   "workspace/demo",
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result.State != StateFailed {
		t.Fatalf("Dispatch().State = %q, want %q", result.State, StateFailed)
	}
	if !strings.Contains(result.Error, "permission denied") {
		t.Fatalf("Dispatch().Error = %q, want contains %q", result.Error, "permission denied")
	}
	if result.RemoteSessionID != "thread_001" {
		t.Fatalf("Dispatch().RemoteSessionID = %q, want %q", result.RemoteSessionID, "thread_001")
	}
	if result.RemoteTaskID != "turn_remote_001" {
		t.Fatalf("Dispatch().RemoteTaskID = %q, want %q", result.RemoteTaskID, "turn_remote_001")
	}
	if result.Metadata["lastError"] != "permission denied" {
		t.Fatalf("Dispatch().Metadata[lastError] = %v, want %q", result.Metadata["lastError"], "permission denied")
	}
}

func TestCodexAppServerBridge_DispatchResumesExistingThreadWhenProvided(t *testing.T) {
	t.Parallel()

	fake := &fakeACPStdioSession{
		readLines: [][]byte{
			[]byte(`{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"codex","version":"0.1.0"}}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thread_existing_001"}}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","id":3,"result":{"turnId":"turn_remote_002"}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thread_existing_001","turnId":"turn_remote_002"}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","method":"item/agentMessage/delta","params":{"threadId":"thread_existing_001","turnId":"turn_remote_002","itemId":"item_002","delta":"continued"}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thread_existing_001","turnId":"turn_remote_002","stopReason":"end_turn"}}` + "\n"),
		},
	}

	bridge := NewCodexAppServerBridge(CodexAppServerBridgeConfig{
		Commands: map[string]CommandConfig{
			"codex": {
				Command: []string{"codex", "app-server"},
			},
		},
		SessionStarter: func(_ context.Context, _ CommandSpec) (ACPStdioSession, error) {
			return fake, nil
		},
	})

	result, err := bridge.Dispatch(context.Background(), DispatchRequest{
		SessionID:   "sess_001",
		TurnID:      "turn_002",
		RunID:       "run_002",
		AgentID:     "default",
		Adapter:     "codex",
		Instruction: "continue task",
		Workspace:   "workspace/demo",
		Context: map[string]any{
			"resume_remote_session_id": "thread_existing_001",
		},
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result.RemoteSessionID != "thread_existing_001" {
		t.Fatalf("Dispatch().RemoteSessionID = %q, want %q", result.RemoteSessionID, "thread_existing_001")
	}
	if len(fake.writtenLines) != 4 {
		t.Fatalf("written lines len = %d, want 4", len(fake.writtenLines))
	}

	var threadReq map[string]any
	if err := json.Unmarshal(fake.writtenLines[2], &threadReq); err != nil {
		t.Fatalf("json.Unmarshal() thread request error = %v", err)
	}
	if threadReq["method"] != "thread/resume" {
		t.Fatalf("thread request method = %v, want thread/resume", threadReq["method"])
	}
	params, ok := threadReq["params"].(map[string]any)
	if !ok {
		t.Fatalf("thread request params type = %T, want map[string]any", threadReq["params"])
	}
	if params["threadId"] != "thread_existing_001" {
		t.Fatalf("thread request params.threadId = %v, want %q", params["threadId"], "thread_existing_001")
	}
}
