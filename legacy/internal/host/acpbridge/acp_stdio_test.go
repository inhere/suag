package acpbridge

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestACPStdioBridge_DispatchRunsACPHandshakeAndPrompt(t *testing.T) {
	t.Parallel()

	fake := &fakeACPStdioSession{
		readLines: [][]byte{
			[]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","id":2,"result":{"sessionId":"remote_sess_001"}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"remote_sess_001","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello "}}}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"remote_sess_001","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"world"}}}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn","userMessageId":"msg_001"}}` + "\n"),
		},
	}

	var gotSpec CommandSpec
	bridge := NewACPStdioBridge(ACPStdioBridgeConfig{
		Commands: map[string]CommandConfig{
			"codex": {
				Command: []string{"codex", "acp"},
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
	if result.Summary != "hello world" {
		t.Fatalf("Dispatch().Summary = %q, want %q", result.Summary, "hello world")
	}
	if result.RemoteSessionID != "remote_sess_001" {
		t.Fatalf("Dispatch().RemoteSessionID = %q, want %q", result.RemoteSessionID, "remote_sess_001")
	}
	if result.RemoteTaskID != "msg_001" {
		t.Fatalf("Dispatch().RemoteTaskID = %q, want %q", result.RemoteTaskID, "msg_001")
	}

	if len(fake.writtenLines) != 3 {
		t.Fatalf("written lines len = %d, want 3", len(fake.writtenLines))
	}
	var initReq map[string]any
	if err := json.Unmarshal(fake.writtenLines[0], &initReq); err != nil {
		t.Fatalf("json.Unmarshal() init request error = %v", err)
	}
	if initReq["method"] != "initialize" {
		t.Fatalf("init request method = %v, want initialize", initReq["method"])
	}

	var newReq map[string]any
	if err := json.Unmarshal(fake.writtenLines[1], &newReq); err != nil {
		t.Fatalf("json.Unmarshal() session/new request error = %v", err)
	}
	if newReq["method"] != "session/new" {
		t.Fatalf("session/new request method = %v, want session/new", newReq["method"])
	}

	var promptReq map[string]any
	if err := json.Unmarshal(fake.writtenLines[2], &promptReq); err != nil {
		t.Fatalf("json.Unmarshal() session/prompt request error = %v", err)
	}
	if promptReq["method"] != "session/prompt" {
		t.Fatalf("session/prompt request method = %v, want session/prompt", promptReq["method"])
	}
}

func TestACPStdioBridge_DispatchReturnsPromptError(t *testing.T) {
	t.Parallel()

	fake := &fakeACPStdioSession{
		readLines: [][]byte{
			[]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","id":2,"result":{"sessionId":"remote_sess_001"}}` + "\n"),
			[]byte(`{"jsonrpc":"2.0","id":3,"error":{"code":-32000,"message":"prompt failed"}}` + "\n"),
		},
	}

	bridge := NewACPStdioBridge(ACPStdioBridgeConfig{
		Commands: map[string]CommandConfig{
			"codex": {
				Command: []string{"codex", "acp"},
			},
		},
		SessionStarter: func(_ context.Context, _ CommandSpec) (ACPStdioSession, error) {
			return fake, nil
		},
	})

	_, err := bridge.Dispatch(context.Background(), DispatchRequest{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Adapter:     "codex",
		Instruction: "analyze project",
		Workspace:   "workspace/demo",
	})
	if err == nil {
		t.Fatal("Dispatch() error = nil, want prompt error")
	}
}

type fakeACPStdioSession struct {
	writtenLines [][]byte
	readLines    [][]byte
	readIndex    int
	closed       bool
}

func (f *fakeACPStdioSession) WriteLine(_ context.Context, line []byte) error {
	f.writtenLines = append(f.writtenLines, append([]byte(nil), line...))
	return nil
}

func (f *fakeACPStdioSession) ReadLine(_ context.Context) ([]byte, error) {
	if f.readIndex >= len(f.readLines) {
		return nil, errors.New("no more lines")
	}
	line := f.readLines[f.readIndex]
	f.readIndex++
	return append([]byte(nil), line...), nil
}

func (f *fakeACPStdioSession) Close() error {
	f.closed = true
	return nil
}
