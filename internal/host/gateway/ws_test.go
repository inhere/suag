package gateway

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/inhere/axon/internal/host/acpbridge"
	"github.com/inhere/axon/internal/host/session"
	_ "modernc.org/sqlite"
)

func TestServer_CreateSessionAndStreamEventsOverWebSocket(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newGatewaySessionStore(t)
	bus := NewEventBus()
	svc := session.NewService(store, bus, &fakeGatewayRunner{}, session.ServiceConfig{
		Now: func() time.Time {
			return time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
		},
	})

	handler := NewHandler(nil, store, svc, bus, nil)
	server := NewServer(":0", handler, bus)
	ts := httptest.NewServer(server.router())
	defer ts.Close()

	createResp := postJSON[createSessionResponse](t, ts.URL+"/api/sessions", map[string]any{
		"title": "demo session",
	})
	if createResp.SessionID == "" {
		t.Fatal("create session response missing session_id")
	}

	wsURL := mustHTTPToWS(t, ts.URL+"/ws?session_id="+url.QueryEscape(createResp.SessionID))
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("wsjson.Read() connected error = %v", err)
	}
	if connected["type"] != "connected" {
		t.Fatalf("connected event type = %v, want connected", connected["type"])
	}

	var snapshot map[string]any
	if err := wsjson.Read(ctx, conn, &snapshot); err != nil {
		t.Fatalf("wsjson.Read() snapshot error = %v", err)
	}
	if snapshot["type"] != "STATE_SNAPSHOT" {
		t.Fatalf("snapshot type = %v, want STATE_SNAPSHOT", snapshot["type"])
	}

	turnResp := postJSON[startTurnResponse](t, ts.URL+"/api/sessions/"+createResp.SessionID+"/turns", map[string]any{
		"agent_id":       "default",
		"message":        "implement feature x",
		"initiator_type": "user",
	})
	if turnResp.RunID == "" {
		t.Fatal("start turn response missing run_id")
	}

	event := readUntilEventType(t, ctx, conn, "CUSTOM")
	if event["name"] != "axon.run.queued" {
		t.Fatalf("event name = %v, want axon.run.queued", event["name"])
	}

	value, ok := event["value"].(map[string]any)
	if !ok {
		t.Fatalf("event value type = %T, want map[string]any", event["value"])
	}
	if value["threadId"] != createResp.SessionID {
		t.Fatalf("event value threadId = %v, want %q", value["threadId"], createResp.SessionID)
	}
	if value["runId"] != turnResp.RunID {
		t.Fatalf("event value runId = %v, want %q", value["runId"], turnResp.RunID)
	}

	textStart := readUntilEventType(t, ctx, conn, "TEXT_MESSAGE_START")
	if textStart["messageId"] == nil {
		t.Fatal("TEXT_MESSAGE_START messageId is nil")
	}

	textContent := readUntilEventType(t, ctx, conn, "TEXT_MESSAGE_CONTENT")
	if textContent["delta"] != "ok" {
		t.Fatalf("TEXT_MESSAGE_CONTENT delta = %v, want %q", textContent["delta"], "ok")
	}

	gotSession := getJSON[getSessionResponse](t, ts.URL+"/api/sessions/"+createResp.SessionID)
	if gotSession.SessionID != createResp.SessionID {
		t.Fatalf("get session session_id = %q, want %q", gotSession.SessionID, createResp.SessionID)
	}
	if gotSession.LastTurnID != turnResp.TurnID {
		t.Fatalf("get session last_turn_id = %q, want %q", gotSession.LastTurnID, turnResp.TurnID)
	}

	events := getJSON[[]Event](t, ts.URL+"/api/sessions/"+createResp.SessionID+"/events")
	if len(events) != 3 {
		t.Fatalf("events len = %d, want 3", len(events))
	}
	if events[0].Type != "run.queued" {
		t.Fatalf("events[0].type = %q, want %q", events[0].Type, "run.queued")
	}
}

func TestServer_SessionSnapshotIncludesExternalTasks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newGatewaySessionStore(t)
	bus := NewEventBus()
	bridge := acpbridge.NewStubBridge(acpbridge.StubBridgeConfig{
		RemoteAgentName: "codex-cli",
	})
	runner := acpbridge.NewRunnerAdapter(bridge, acpbridge.RunnerAdapterConfig{
		AdapterName: "codex",
		Workspace:   "workspace/demo",
	})
	svc := session.NewService(store, bus, runner, session.ServiceConfig{
		Now: func() time.Time {
			return time.Date(2026, 4, 11, 19, 0, 0, 0, time.UTC)
		},
	})

	handler := NewHandler(nil, store, svc, bus, nil)
	server := NewServer(":0", handler, bus)
	ts := httptest.NewServer(server.router())
	defer ts.Close()

	createResp := postJSON[createSessionResponse](t, ts.URL+"/api/sessions", map[string]any{
		"title": "snapshot external tasks",
	})
	turnResp := postJSON[startTurnResponse](t, ts.URL+"/api/sessions/"+createResp.SessionID+"/turns", map[string]any{
		"agent_id":       "default",
		"message":        "delegate to codex",
		"initiator_type": "user",
	})
	if turnResp.RunID == "" {
		t.Fatal("start turn response missing run_id")
	}

	sessionResp := getJSON[getSessionResponse](t, ts.URL+"/api/sessions/"+createResp.SessionID)
	if len(sessionResp.ExternalTasks) != 1 {
		t.Fatalf("get session external_tasks len = %d, want 1", len(sessionResp.ExternalTasks))
	}
	if sessionResp.ExternalTasks[0].RunID != turnResp.RunID {
		t.Fatalf("get session external_tasks[0].run_id = %q, want %q", sessionResp.ExternalTasks[0].RunID, turnResp.RunID)
	}

	wsURL := mustHTTPToWS(t, ts.URL+"/ws?session_id="+url.QueryEscape(createResp.SessionID))
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("wsjson.Read() connected error = %v", err)
	}

	var snapshot map[string]any
	if err := wsjson.Read(ctx, conn, &snapshot); err != nil {
		t.Fatalf("wsjson.Read() snapshot error = %v", err)
	}
	if snapshot["type"] != "STATE_SNAPSHOT" {
		t.Fatalf("snapshot type = %v, want STATE_SNAPSHOT", snapshot["type"])
	}

	rawSnapshot, ok := snapshot["snapshot"].(map[string]any)
	if !ok {
		t.Fatalf("snapshot.snapshot type = %T, want map[string]any", snapshot["snapshot"])
	}
	rawExternalTasks, ok := rawSnapshot["externalTasks"].([]any)
	if !ok {
		t.Fatalf("snapshot.snapshot.externalTasks type = %T, want []any", rawSnapshot["externalTasks"])
	}
	if len(rawExternalTasks) != 1 {
		t.Fatalf("snapshot.snapshot.externalTasks len = %d, want 1", len(rawExternalTasks))
	}
	task, ok := rawExternalTasks[0].(map[string]any)
	if !ok {
		t.Fatalf("snapshot.snapshot.externalTasks[0] type = %T, want map[string]any", rawExternalTasks[0])
	}
	if task["remoteAgentName"] != "codex-cli" {
		t.Fatalf("snapshot.snapshot.externalTasks[0].remoteAgentName = %v, want %q", task["remoteAgentName"], "codex-cli")
	}
}

func TestServer_SessionSnapshotIncludesExternalTaskMetadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newGatewaySessionStore(t)
	bus := NewEventBus()
	runner := acpbridge.NewRunnerAdapter(&gatewayMetadataBridge{}, acpbridge.RunnerAdapterConfig{
		AdapterName: "codex",
		Workspace:   "workspace/demo",
	})
	svc := session.NewService(store, bus, runner, session.ServiceConfig{
		Now: func() time.Time {
			return time.Date(2026, 4, 12, 10, 0, 0, 0, time.UTC)
		},
	})

	handler := NewHandler(nil, store, svc, bus, nil)
	server := NewServer(":0", handler, bus)
	ts := httptest.NewServer(server.router())
	defer ts.Close()

	createResp := postJSON[createSessionResponse](t, ts.URL+"/api/sessions", map[string]any{
		"title": "snapshot external task metadata",
	})
	postJSON[startTurnResponse](t, ts.URL+"/api/sessions/"+createResp.SessionID+"/turns", map[string]any{
		"agent_id":       "default",
		"message":        "delegate with metadata",
		"initiator_type": "user",
	})

	sessionResp := getJSON[getSessionResponse](t, ts.URL+"/api/sessions/"+createResp.SessionID)
	if len(sessionResp.ExternalTasks) != 1 {
		t.Fatalf("get session external_tasks len = %d, want 1", len(sessionResp.ExternalTasks))
	}
	if sessionResp.ExternalTasks[0].Metadata["protocol"] != "codex_app_server" {
		t.Fatalf("get session external_tasks[0].metadata[protocol] = %v, want %q", sessionResp.ExternalTasks[0].Metadata["protocol"], "codex_app_server")
	}

	wsURL := mustHTTPToWS(t, ts.URL+"/ws?session_id="+url.QueryEscape(createResp.SessionID))
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("wsjson.Read() connected error = %v", err)
	}

	var snapshot map[string]any
	if err := wsjson.Read(ctx, conn, &snapshot); err != nil {
		t.Fatalf("wsjson.Read() snapshot error = %v", err)
	}

	rawSnapshot, ok := snapshot["snapshot"].(map[string]any)
	if !ok {
		t.Fatalf("snapshot.snapshot type = %T, want map[string]any", snapshot["snapshot"])
	}
	rawExternalTasks, ok := rawSnapshot["externalTasks"].([]any)
	if !ok {
		t.Fatalf("snapshot.snapshot.externalTasks type = %T, want []any", rawSnapshot["externalTasks"])
	}
	task, ok := rawExternalTasks[0].(map[string]any)
	if !ok {
		t.Fatalf("snapshot.snapshot.externalTasks[0] type = %T, want map[string]any", rawExternalTasks[0])
	}
	metadata, ok := task["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("snapshot.snapshot.externalTasks[0].metadata type = %T, want map[string]any", task["metadata"])
	}
	if metadata["protocol"] != "codex_app_server" {
		t.Fatalf("snapshot.snapshot.externalTasks[0].metadata[protocol] = %v, want %q", metadata["protocol"], "codex_app_server")
	}
}

func TestServer_SessionSnapshotAggregatesExternalStreamState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newGatewaySessionStore(t)
	bus := NewEventBus()
	svc := session.NewService(store, bus, &emittingGatewayRunner{}, session.ServiceConfig{
		Now: func() time.Time {
			return time.Date(2026, 4, 12, 15, 0, 0, 0, time.UTC)
		},
	})

	handler := NewHandler(nil, store, svc, bus, nil)
	server := NewServer(":0", handler, bus)
	ts := httptest.NewServer(server.router())
	defer ts.Close()

	createResp := postJSON[createSessionResponse](t, ts.URL+"/api/sessions", map[string]any{
		"title": "snapshot external stream state",
	})
	postJSON[startTurnResponse](t, ts.URL+"/api/sessions/"+createResp.SessionID+"/turns", map[string]any{
		"agent_id":       "default",
		"message":        "stream runtime events",
		"initiator_type": "user",
	})

	wsURL := mustHTTPToWS(t, ts.URL+"/ws?session_id="+url.QueryEscape(createResp.SessionID))
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("wsjson.Read() connected error = %v", err)
	}

	var snapshot map[string]any
	if err := wsjson.Read(ctx, conn, &snapshot); err != nil {
		t.Fatalf("wsjson.Read() snapshot error = %v", err)
	}

	rawSnapshot, ok := snapshot["snapshot"].(map[string]any)
	if !ok {
		t.Fatalf("snapshot.snapshot type = %T, want map[string]any", snapshot["snapshot"])
	}

	rawExternalActivities, ok := rawSnapshot["externalActivities"].([]any)
	if !ok {
		t.Fatalf("snapshot.snapshot.externalActivities type = %T, want []any", rawSnapshot["externalActivities"])
	}
	if len(rawExternalActivities) != 1 {
		t.Fatalf("snapshot.snapshot.externalActivities len = %d, want 1", len(rawExternalActivities))
	}
	activity, ok := rawExternalActivities[0].(map[string]any)
	if !ok {
		t.Fatalf("snapshot.snapshot.externalActivities[0] type = %T, want map[string]any", rawExternalActivities[0])
	}
	if activity["lastKind"] != "item_started" {
		t.Fatalf("snapshot.snapshot.externalActivities[0].lastKind = %v, want %q", activity["lastKind"], "item_started")
	}
	if activity["updatedAt"] != "2026-04-12T15:00:00Z" {
		t.Fatalf("snapshot.snapshot.externalActivities[0].updatedAt = %v, want %q", activity["updatedAt"], "2026-04-12T15:00:00Z")
	}

	rawExternalMessages, ok := rawSnapshot["externalMessages"].([]any)
	if !ok {
		t.Fatalf("snapshot.snapshot.externalMessages type = %T, want []any", rawSnapshot["externalMessages"])
	}
	if len(rawExternalMessages) != 1 {
		t.Fatalf("snapshot.snapshot.externalMessages len = %d, want 1", len(rawExternalMessages))
	}
	message, ok := rawExternalMessages[0].(map[string]any)
	if !ok {
		t.Fatalf("snapshot.snapshot.externalMessages[0] type = %T, want map[string]any", rawExternalMessages[0])
	}
	if message["content"] != "thinking..." {
		t.Fatalf("snapshot.snapshot.externalMessages[0].content = %v, want %q", message["content"], "thinking...")
	}
	if message["started"] != true {
		t.Fatalf("snapshot.snapshot.externalMessages[0].started = %v, want true", message["started"])
	}
	if message["completed"] != true {
		t.Fatalf("snapshot.snapshot.externalMessages[0].completed = %v, want true", message["completed"])
	}
	if message["updatedAt"] != "2026-04-12T15:00:00Z" {
		t.Fatalf("snapshot.snapshot.externalMessages[0].updatedAt = %v, want %q", message["updatedAt"], "2026-04-12T15:00:00Z")
	}

	rawTurns, ok := rawSnapshot["turns"].([]any)
	if !ok {
		t.Fatalf("snapshot.snapshot.turns type = %T, want []any", rawSnapshot["turns"])
	}
	if len(rawTurns) != 1 {
		t.Fatalf("snapshot.snapshot.turns len = %d, want 1", len(rawTurns))
	}
	turn, ok := rawTurns[0].(map[string]any)
	if !ok {
		t.Fatalf("snapshot.snapshot.turns[0] type = %T, want map[string]any", rawTurns[0])
	}
	if turn["runCount"] != float64(1) {
		t.Fatalf("snapshot.snapshot.turns[0].runCount = %v, want %v", turn["runCount"], float64(1))
	}
	if turn["latestEventType"] != session.EventRunFinished {
		t.Fatalf("snapshot.snapshot.turns[0].latestEventType = %v, want %q", turn["latestEventType"], session.EventRunFinished)
	}
	if turn["displayState"] != "completed" {
		t.Fatalf("snapshot.snapshot.turns[0].displayState = %v, want %q", turn["displayState"], "completed")
	}
	if turn["hasExternalWork"] != true {
		t.Fatalf("snapshot.snapshot.turns[0].hasExternalWork = %v, want true", turn["hasExternalWork"])
	}
	activeRunIDs, ok := turn["activeRunIds"].([]any)
	if !ok {
		t.Fatalf("snapshot.snapshot.turns[0].activeRunIds type = %T, want []any", turn["activeRunIds"])
	}
	if len(activeRunIDs) != 0 {
		t.Fatalf("snapshot.snapshot.turns[0].activeRunIds len = %d, want 0", len(activeRunIDs))
	}
	completedRunIDs, ok := turn["completedRunIds"].([]any)
	if !ok {
		t.Fatalf("snapshot.snapshot.turns[0].completedRunIds type = %T, want []any", turn["completedRunIds"])
	}
	if len(completedRunIDs) != 1 {
		t.Fatalf("snapshot.snapshot.turns[0].completedRunIds len = %d, want 1", len(completedRunIDs))
	}
	failedRunIDs, ok := turn["failedRunIds"].([]any)
	if !ok {
		t.Fatalf("snapshot.snapshot.turns[0].failedRunIds type = %T, want []any", turn["failedRunIds"])
	}
	if len(failedRunIDs) != 0 {
		t.Fatalf("snapshot.snapshot.turns[0].failedRunIds len = %d, want 0", len(failedRunIDs))
	}

	rawRuns, ok := rawSnapshot["runs"].([]any)
	if !ok {
		t.Fatalf("snapshot.snapshot.runs type = %T, want []any", rawSnapshot["runs"])
	}
	if len(rawRuns) != 1 {
		t.Fatalf("snapshot.snapshot.runs len = %d, want 1", len(rawRuns))
	}
	run, ok := rawRuns[0].(map[string]any)
	if !ok {
		t.Fatalf("snapshot.snapshot.runs[0] type = %T, want map[string]any", rawRuns[0])
	}
	if run["eventCount"] != float64(6) {
		t.Fatalf("snapshot.snapshot.runs[0].eventCount = %v, want %v", run["eventCount"], float64(6))
	}
	if run["displayState"] != "completed" {
		t.Fatalf("snapshot.snapshot.runs[0].displayState = %v, want %q", run["displayState"], "completed")
	}
	runID, _ := run["runId"].(string)
	wantMessageID := runID + ":assistant"
	if run["latestMessageId"] != wantMessageID {
		t.Fatalf("snapshot.snapshot.runs[0].latestMessageId = %v, want %q", run["latestMessageId"], wantMessageID)
	}
	if run["latestMessageText"] != "done" {
		t.Fatalf("snapshot.snapshot.runs[0].latestMessageText = %v, want %q", run["latestMessageText"], "done")
	}
	if run["summary"] != "done" {
		t.Fatalf("snapshot.snapshot.runs[0].summary = %v, want %q", run["summary"], "done")
	}
	externalTaskIDs, ok := run["externalTaskIds"].([]any)
	if !ok {
		t.Fatalf("snapshot.snapshot.runs[0].externalTaskIds type = %T, want []any", run["externalTaskIds"])
	}
	if len(externalTaskIDs) != 0 {
		t.Fatalf("snapshot.snapshot.runs[0].externalTaskIds len = %d, want 0", len(externalTaskIDs))
	}
	if run["updatedAt"] != "2026-04-12T15:00:00Z" {
		t.Fatalf("snapshot.snapshot.runs[0].updatedAt = %v, want %q", run["updatedAt"], "2026-04-12T15:00:00Z")
	}
}

type gatewayMetadataBridge struct{}

func (b *gatewayMetadataBridge) Dispatch(_ context.Context, _ acpbridge.DispatchRequest) (acpbridge.DispatchResult, error) {
	return acpbridge.DispatchResult{
		State:           acpbridge.StateCompleted,
		Summary:         "done",
		RemoteAgentName: "codex-cli",
		RemoteSessionID: "thread_001",
		RemoteTaskID:    "turn_remote_001",
		LastHeartbeatAt: time.Date(2026, 4, 12, 10, 0, 0, 0, time.UTC),
		Metadata: map[string]any{
			"protocol": "codex_app_server",
			"eventSummary": map[string]any{
				"messageDeltaCount": 2,
			},
		},
	}, nil
}

func TestServer_ContinueTurnOverWebSocketPublishesEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newGatewaySessionStore(t)
	bus := NewEventBus()
	svc := session.NewService(store, bus, &fakeGatewayRunner{}, session.ServiceConfig{
		Now: func() time.Time {
			return time.Date(2026, 4, 11, 12, 30, 0, 0, time.UTC)
		},
	})

	handler := NewHandler(nil, store, svc, bus, nil)
	server := NewServer(":0", handler, bus)
	ts := httptest.NewServer(server.router())
	defer ts.Close()

	createResp := postJSON[createSessionResponse](t, ts.URL+"/api/sessions", map[string]any{
		"title": "continued session",
	})

	wsURL := mustHTTPToWS(t, ts.URL+"/ws?session_id="+url.QueryEscape(createResp.SessionID))
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("wsjson.Read() connected error = %v", err)
	}

	var snapshot map[string]any
	if err := wsjson.Read(ctx, conn, &snapshot); err != nil {
		t.Fatalf("wsjson.Read() snapshot error = %v", err)
	}
	if snapshot["type"] != "STATE_SNAPSHOT" {
		t.Fatalf("snapshot type = %v, want STATE_SNAPSHOT", snapshot["type"])
	}

	if err := wsjson.Write(ctx, conn, map[string]any{
		"type":           "continue_turn",
		"session_id":     createResp.SessionID,
		"message":        "please continue",
		"agent_id":       "default",
		"initiator_type": "user",
	}); err != nil {
		t.Fatalf("wsjson.Write() command error = %v", err)
	}

	var ack map[string]any
	if err := wsjson.Read(ctx, conn, &ack); err != nil {
		t.Fatalf("wsjson.Read() ack error = %v", err)
	}
	if ack["type"] != "command.accepted" {
		t.Fatalf("ack type = %v, want command.accepted", ack["type"])
	}

	event := readUntilEventType(t, ctx, conn, "CUSTOM")

	value, ok := event["value"].(map[string]any)
	if !ok {
		t.Fatalf("event value type = %T, want map[string]any", event["value"])
	}
	if value["threadId"] != createResp.SessionID {
		t.Fatalf("event value threadId = %v, want %q", value["threadId"], createResp.SessionID)
	}

	gotSession := getJSON[getSessionResponse](t, ts.URL+"/api/sessions/"+createResp.SessionID)
	if gotSession.LastTurnID == "" {
		t.Fatal("get session last_turn_id is empty after continue_turn")
	}

	events := getJSON[[]Event](t, ts.URL+"/api/sessions/"+createResp.SessionID+"/events")
	if len(events) != 3 {
		t.Fatalf("events len = %d, want 3", len(events))
	}
}

func TestServer_ContinueTurnReusesPreviousRemoteSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newGatewaySessionStore(t)
	bus := NewEventBus()
	runner := &resumeAwareGatewayRunner{}
	svc := session.NewService(store, bus, runner, session.ServiceConfig{
		Now: func() time.Time {
			return time.Date(2026, 4, 12, 12, 0, 0, 0, time.UTC)
		},
	})

	handler := NewHandler(nil, store, svc, bus, nil)
	server := NewServer(":0", handler, bus)
	ts := httptest.NewServer(server.router())
	defer ts.Close()

	createResp := postJSON[createSessionResponse](t, ts.URL+"/api/sessions", map[string]any{
		"title": "continue turn remote session reuse",
	})

	postJSON[startTurnResponse](t, ts.URL+"/api/sessions/"+createResp.SessionID+"/turns", map[string]any{
		"agent_id":       "default",
		"message":        "start delegated turn",
		"initiator_type": "user",
	})

	wsURL := mustHTTPToWS(t, ts.URL+"/ws?session_id="+url.QueryEscape(createResp.SessionID))
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("wsjson.Read() connected error = %v", err)
	}

	var snapshot map[string]any
	if err := wsjson.Read(ctx, conn, &snapshot); err != nil {
		t.Fatalf("wsjson.Read() snapshot error = %v", err)
	}
	if snapshot["type"] != "STATE_SNAPSHOT" {
		t.Fatalf("snapshot type = %v, want STATE_SNAPSHOT", snapshot["type"])
	}

	if err := wsjson.Write(ctx, conn, map[string]any{
		"type":           "continue_turn",
		"session_id":     createResp.SessionID,
		"message":        "continue delegated turn",
		"agent_id":       "default",
		"initiator_type": "user",
	}); err != nil {
		t.Fatalf("wsjson.Write() command error = %v", err)
	}

	var ack map[string]any
	if err := wsjson.Read(ctx, conn, &ack); err != nil {
		t.Fatalf("wsjson.Read() ack error = %v", err)
	}
	if ack["type"] != "command.accepted" {
		t.Fatalf("ack type = %v, want command.accepted", ack["type"])
	}

	readUntilEventType(t, ctx, conn, "CUSTOM")

	waitForGatewayRunnerRequests(t, runner, 2)
	if runner.requests[1].Context["resume_turn_id"] == nil {
		t.Fatal("continue request missing resume_turn_id")
	}
	if runner.requests[1].Context["resume_external_tasks"] == nil {
		t.Fatal("continue request missing resume_external_tasks")
	}
	if runner.requests[1].Context["external_task_id"] == nil {
		t.Fatal("continue request missing external_task_id")
	}
}

func TestServer_StartTurnStreamsExternalRuntimeEvents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newGatewaySessionStore(t)
	bus := NewEventBus()
	svc := session.NewService(store, bus, &emittingGatewayRunner{}, session.ServiceConfig{
		Now: func() time.Time {
			return time.Date(2026, 4, 12, 14, 0, 0, 0, time.UTC)
		},
	})

	handler := NewHandler(nil, store, svc, bus, nil)
	server := NewServer(":0", handler, bus)
	ts := httptest.NewServer(server.router())
	defer ts.Close()

	createResp := postJSON[createSessionResponse](t, ts.URL+"/api/sessions", map[string]any{
		"title": "stream external runtime events",
	})

	wsURL := mustHTTPToWS(t, ts.URL+"/ws?session_id="+url.QueryEscape(createResp.SessionID))
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("wsjson.Read() connected error = %v", err)
	}

	var snapshot map[string]any
	if err := wsjson.Read(ctx, conn, &snapshot); err != nil {
		t.Fatalf("wsjson.Read() snapshot error = %v", err)
	}

	postJSON[startTurnResponse](t, ts.URL+"/api/sessions/"+createResp.SessionID+"/turns", map[string]any{
		"agent_id":       "default",
		"message":        "stream runtime events",
		"initiator_type": "user",
	})

	foundExternalActivity := false
	foundExternalText := false
	for i := 0; i < 12; i++ {
		var message map[string]any
		if err := wsjson.Read(ctx, conn, &message); err != nil {
			t.Fatalf("wsjson.Read() event error = %v", err)
		}
		if message["type"] == "CUSTOM" && message["name"] == "axon.run.external.activity" {
			value, _ := message["value"].(map[string]any)
			if value != nil && value["externalTaskId"] != "" {
				foundExternalActivity = true
			}
		}
		if message["type"] == "TEXT_MESSAGE_CONTENT" && message["delta"] == "thinking..." {
			foundExternalText = true
		}
		if foundExternalActivity && foundExternalText {
			break
		}
	}
	if !foundExternalActivity {
		t.Fatal("did not receive external activity event")
	}
	if !foundExternalText {
		t.Fatal("did not receive external text delta event")
	}
}

func readUntilEventType(t *testing.T, ctx context.Context, conn *websocket.Conn, wantType string) map[string]any {
	t.Helper()

	for i := 0; i < 12; i++ {
		var message map[string]any
		if err := wsjson.Read(ctx, conn, &message); err != nil {
			t.Fatalf("wsjson.Read() error = %v", err)
		}
		if message["type"] == wantType {
			return message
		}
	}

	t.Fatalf("did not receive event type %q within expected reads", wantType)
	return nil
}

type fakeGatewayRunner struct{}

func (fakeGatewayRunner) Run(context.Context, session.RuntimeRequest) (session.RuntimeResult, error) {
	return session.RuntimeResult{Summary: "ok"}, nil
}

type resumeAwareGatewayRunner struct {
	requests []session.RuntimeRequest
}

func (r *resumeAwareGatewayRunner) Run(_ context.Context, in session.RuntimeRequest) (session.RuntimeResult, error) {
	r.requests = append(r.requests, in)

	if len(r.requests) == 1 {
		return session.RuntimeResult{
			Summary: "started",
			ExternalTask: &session.ExternalTask{
				Adapter:         "codex",
				RemoteAgentName: "codex-cli",
				RemoteSessionID: "thread_001",
				RemoteTaskID:    "turn_remote_001",
				State:           "completed",
			},
		}, nil
	}

	return session.RuntimeResult{
		Summary: "continued",
		ExternalTask: &session.ExternalTask{
			Adapter:         "codex",
			RemoteAgentName: "codex-cli",
			RemoteSessionID: "thread_001",
			RemoteTaskID:    "turn_remote_002",
			State:           "completed",
		},
	}, nil
}

type emittingGatewayRunner struct{}

func (r *emittingGatewayRunner) Run(_ context.Context, in session.RuntimeRequest) (session.RuntimeResult, error) {
	if in.Emit != nil {
		_ = in.Emit(session.RuntimeEvent{
			EventType: session.EventRunExternalActivity,
			Payload: map[string]any{
				"kind":   "item_started",
				"itemId": "item_001",
			},
		})
		_ = in.Emit(session.RuntimeEvent{
			EventType: session.EventRunExternalMessageDelta,
			Payload: map[string]any{
				"messageId":    "turn_remote_001:item_001",
				"delta":        "thinking...",
				"isFirstChunk": true,
			},
		})
		_ = in.Emit(session.RuntimeEvent{
			EventType: session.EventRunExternalMessageEnd,
			Payload: map[string]any{
				"messageId": "turn_remote_001:item_001",
			},
		})
	}
	return session.RuntimeResult{
		Summary: "done",
	}, nil
}

func waitForGatewayRunnerRequests(t *testing.T, runner *resumeAwareGatewayRunner, want int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(runner.requests) >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("runner requests len = %d, want at least %d", len(runner.requests), want)
}

func newGatewaySessionStore(t *testing.T) *session.SQLiteStore {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "gateway-session.db")
	applyGatewaySessionMigration(t, dbPath)

	store, err := session.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("session.NewSQLiteStore() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("store.Close() error = %v", err)
		}
	})
	return store
}

func applyGatewaySessionMigration(t *testing.T, dbPath string) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("db.Close() error = %v", err)
		}
	}()

	upSQL := readGatewaySessionMigrationUp(t)
	if _, err := db.Exec(upSQL); err != nil {
		t.Fatalf("db.Exec() migration error = %v", err)
	}
}

func readGatewaySessionMigrationUp(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}

	migrationPath := filepath.Join(
		filepath.Dir(file),
		"..", "..", "..",
		"db", "migrations", "20260411-100000-create-host-session-foundation.sql",
	)
	content, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}

	parts := strings.Split(string(content), "-- Migrate:DOWN")
	if len(parts) != 2 {
		t.Fatalf("unexpected migration format in %s", migrationPath)
	}

	upSQL := strings.TrimSpace(strings.TrimPrefix(parts[0], "-- Migrate:UP"))
	if upSQL == "" {
		t.Fatalf("empty migration up section in %s", migrationPath)
	}
	return upSQL
}

func postJSON[T any](t *testing.T, endpoint string, payload any) T {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("http.Post() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("POST %s status = %d", endpoint, resp.StatusCode)
	}

	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("json.NewDecoder().Decode() error = %v", err)
	}
	return out
}

func getJSON[T any](t *testing.T, endpoint string) T {
	t.Helper()

	resp, err := http.Get(endpoint)
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("GET %s status = %d", endpoint, resp.StatusCode)
	}

	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("json.NewDecoder().Decode() error = %v", err)
	}
	return out
}

func mustHTTPToWS(t *testing.T, raw string) string {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	if parsed.Scheme == "https" {
		parsed.Scheme = "wss"
	} else {
		parsed.Scheme = "ws"
	}
	return parsed.String()
}
