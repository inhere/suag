package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/inhere/axon/internal/ops/hitl"
)

func TestService_StartTurnCreatesQueuedRunAndPublishesEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{
			Summary: "run-output",
		},
	}
	now := time.Date(2026, 4, 11, 11, 0, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	result, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_001",
		AgentID:       "default",
		UserMessage:   "implement feature x",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	if result.SessionID != "sess_001" {
		t.Fatalf("StartTurn().SessionID = %q, want %q", result.SessionID, "sess_001")
	}
	if result.TurnID == "" {
		t.Fatal("StartTurn().TurnID is empty")
	}
	if result.RunID == "" {
		t.Fatal("StartTurn().RunID is empty")
	}
	if len(bus.events) == 0 {
		t.Fatal("publisher received no events")
	}
	if bus.events[0].EventType != EventRunQueued {
		t.Fatalf("first published event type = %q, want %q", bus.events[0].EventType, EventRunQueued)
	}
	if len(bus.events) != 3 {
		t.Fatalf("published events len = %d, want 3", len(bus.events))
	}
	if bus.events[1].EventType != EventRunStarted {
		t.Fatalf("second published event type = %q, want %q", bus.events[1].EventType, EventRunStarted)
	}
	if bus.events[2].EventType != EventRunFinished {
		t.Fatalf("third published event type = %q, want %q", bus.events[2].EventType, EventRunFinished)
	}
	if runner.lastRequest.SessionID != "sess_001" {
		t.Fatalf("runner request SessionID = %q, want %q", runner.lastRequest.SessionID, "sess_001")
	}
	if runner.lastRequest.TurnID != result.TurnID {
		t.Fatalf("runner request TurnID = %q, want %q", runner.lastRequest.TurnID, result.TurnID)
	}
	if runner.lastRequest.RunID != result.RunID {
		t.Fatalf("runner request RunID = %q, want %q", runner.lastRequest.RunID, result.RunID)
	}

	gotSession, err := store.GetSession(ctx, "sess_001")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if gotSession.LastTurnID != result.TurnID {
		t.Fatalf("GetSession().LastTurnID = %q, want %q", gotSession.LastTurnID, result.TurnID)
	}

	events, err := store.ListEventsBySession(ctx, "sess_001", 0)
	if err != nil {
		t.Fatalf("ListEventsBySession() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("ListEventsBySession() len = %d, want 3", len(events))
	}
	if events[2].RunID != result.RunID {
		t.Fatalf("ListEventsBySession()[2].RunID = %q, want %q", events[2].RunID, result.RunID)
	}
}

func TestService_StartTurnStoresInitialLoopMetadataOnTerminalRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{
			Summary: "run-output",
		},
	}
	now := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	result, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_loop_initial",
		AgentID:       "default",
		UserMessage:   "first run",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	runModel, err := store.GetRun(ctx, result.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if runModel.Metadata["loop_phase"] != "initial" {
		t.Fatalf("run metadata loop_phase = %v, want %q", runModel.Metadata["loop_phase"], "initial")
	}
	if runModel.Metadata["continue_mode"] != "start" {
		t.Fatalf("run metadata continue_mode = %v, want %q", runModel.Metadata["continue_mode"], "start")
	}
	if runModel.Metadata["turn_outcome"] != "completed" {
		t.Fatalf("run metadata turn_outcome = %v, want %q", runModel.Metadata["turn_outcome"], "completed")
	}
}

func TestService_StartTurnBuildsSessionMetadataBeforeRuntimeContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{Summary: "run-output"},
	}

	var runtimeInput RuntimeContextBuildInput
	svc := NewService(store, bus, runner, ServiceConfig{
		SessionMetadataBuilder: func(input SessionMetadataBuildInput) map[string]any {
			return map[string]any{
				"capability_set": map[string]any{
					"tools": []map[string]any{
						{"name": "file_read", "source": "native", "provider_id": "native"},
					},
				},
			}
		},
		RuntimeContextBuilder: func(input RuntimeContextBuildInput) map[string]any {
			runtimeInput = input
			return map[string]any{
				"resolved_tool_names": []string{"file_read"},
			}
		},
	})

	result, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_capability_session",
		AgentID:       "default",
		UserMessage:   "first message",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	sessionModel, err := store.GetSession(ctx, "sess_capability_session")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	capabilitySet, ok := sessionModel.Metadata["capability_set"].(map[string]any)
	if !ok {
		t.Fatalf("session metadata capability_set type = %T, want map[string]any", sessionModel.Metadata["capability_set"])
	}
	toolsMeta, ok := capabilitySet["tools"].([]any)
	if !ok {
		t.Fatalf("session metadata capability_set[tools] type = %T, want []any", capabilitySet["tools"])
	}
	firstTool, ok := toolsMeta[0].(map[string]any)
	if !ok {
		t.Fatalf("session metadata capability_set[tools][0] type = %T, want map[string]any", toolsMeta[0])
	}
	if len(toolsMeta) != 1 || firstTool["name"] != "file_read" {
		t.Fatalf("session capability tools = %#v, want file_read", toolsMeta)
	}

	if runtimeInput.SessionID != "sess_capability_session" {
		t.Fatalf("runtime builder SessionID = %q, want %q", runtimeInput.SessionID, "sess_capability_session")
	}
	if runtimeInput.AgentID != "default" {
		t.Fatalf("runtime builder AgentID = %q, want %q", runtimeInput.AgentID, "default")
	}
	if runtimeInput.Message != "first message" {
		t.Fatalf("runtime builder Message = %q, want %q", runtimeInput.Message, "first message")
	}
	if runtimeInput.IsContinue {
		t.Fatal("runtime builder IsContinue = true, want false")
	}
	if _, ok := runtimeInput.SessionMetadata["capability_set"].(map[string]any); !ok {
		t.Fatalf("runtime builder SessionMetadata[capability_set] type = %T, want map[string]any", runtimeInput.SessionMetadata["capability_set"])
	}
	if names, ok := runner.lastRequest.Context["resolved_tool_names"].([]string); !ok || len(names) != 1 || names[0] != "file_read" {
		t.Fatalf("runner context resolved_tool_names = %#v, want [file_read]", runner.lastRequest.Context["resolved_tool_names"])
	}
	if result.RunID == "" {
		t.Fatal("StartTurn().RunID is empty")
	}
}

func TestService_ContinueTurnCreatesNewTurnForExistingWaitingSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	now := time.Date(2026, 4, 11, 11, 30, 0, 0, time.UTC)

	if err := store.CreateSession(ctx, Session{
		SessionID: "sess_001",
		Title:     "existing session",
		Status:    SessionOpen,
		CreatedAt: now,
		UpdatedAt: now,
		Metadata:  map[string]any{},
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	bus := newFakePublisher()
	firstRunner := &fakeRunner{
		result: RuntimeResult{
			Summary:   "await_resume",
			WaitState: RunInputRequired,
		},
		emitEvents: []RuntimeEvent{
			{
				EventType: EventRunStatus,
				Payload: map[string]any{
					"state": string(RunInputRequired),
				},
			},
		},
	}
	svc := NewService(store, bus, firstRunner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	_, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_001",
		AgentID:       "default",
		UserMessage:   "need more input",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	secondRunner := &fakeRunner{
		result: RuntimeResult{Summary: "continued"},
	}
	svc.runner = secondRunner
	svc.now = func() time.Time { return now.Add(5 * time.Minute) }

	result, err := svc.ContinueTurn(ctx, ContinueTurnInput{
		SessionID:     "sess_001",
		AgentID:       "default",
		Message:       "please continue",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("ContinueTurn() error = %v", err)
	}
	if result.SessionID != "sess_001" {
		t.Fatalf("ContinueTurn().SessionID = %q, want %q", result.SessionID, "sess_001")
	}
	if result.TurnID == "" {
		t.Fatal("ContinueTurn().TurnID is empty")
	}
	if secondRunner.lastRequest.Instruction != "please continue" {
		t.Fatalf("runner request Instruction = %q, want %q", secondRunner.lastRequest.Instruction, "please continue")
	}

	gotSession, err := store.GetSession(ctx, "sess_001")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if gotSession.LastTurnID != result.TurnID {
		t.Fatalf("GetSession().LastTurnID = %q, want %q", gotSession.LastTurnID, result.TurnID)
	}
	if len(bus.events) != 6 {
		t.Fatalf("published events len = %d, want 6", len(bus.events))
	}
	if bus.events[0].Sequence != 1 {
		t.Fatalf("bus.events[0].Sequence = %d, want %d", bus.events[0].Sequence, 1)
	}
	if bus.events[1].Sequence != 2 {
		t.Fatalf("bus.events[1].Sequence = %d, want %d", bus.events[1].Sequence, 2)
	}
	if bus.events[5].Sequence != 6 {
		t.Fatalf("bus.events[5].Sequence = %d, want %d", bus.events[5].Sequence, 6)
	}
}

func TestService_ContinueTurnStoresResumedLoopMetadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	now := time.Date(2026, 4, 20, 9, 30, 0, 0, time.UTC)

	waitingRunner := &fakeRunner{
		result: RuntimeResult{
			Summary:   "await_resume",
			WaitState: RunInputRequired,
		},
		emitEvents: []RuntimeEvent{
			{
				EventType: EventRunStatus,
				Payload: map[string]any{
					"state": string(RunInputRequired),
				},
			},
		},
	}
	svc := NewService(store, bus, waitingRunner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	firstResult, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_loop_resume",
		AgentID:       "default",
		UserMessage:   "need more input",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	svc.runner = &fakeRunner{
		result: RuntimeResult{
			Summary: "continued",
		},
	}
	svc.now = func() time.Time { return now.Add(2 * time.Minute) }

	secondResult, err := svc.ContinueTurn(ctx, ContinueTurnInput{
		SessionID:     "sess_loop_resume",
		AgentID:       "default",
		Message:       "please continue",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("ContinueTurn() error = %v", err)
	}

	runModel, err := store.GetRun(ctx, secondResult.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if runModel.ResumeFromRunID != firstResult.RunID {
		t.Fatalf("run ResumeFromRunID = %q, want %q", runModel.ResumeFromRunID, firstResult.RunID)
	}
	if runModel.Metadata["loop_phase"] != "resumed" {
		t.Fatalf("run metadata loop_phase = %v, want %q", runModel.Metadata["loop_phase"], "resumed")
	}
	if runModel.Metadata["resume_source"] != "continue_turn" {
		t.Fatalf("run metadata resume_source = %v, want %q", runModel.Metadata["resume_source"], "continue_turn")
	}
	if runModel.Metadata["continue_mode"] != "message" {
		t.Fatalf("run metadata continue_mode = %v, want %q", runModel.Metadata["continue_mode"], "message")
	}
	if runModel.Metadata["turn_outcome"] != "completed" {
		t.Fatalf("run metadata turn_outcome = %v, want %q", runModel.Metadata["turn_outcome"], "completed")
	}
}

func TestService_ContinueTurnBuildsRuntimeContextWithSessionMetadataAndMessage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	now := time.Date(2026, 4, 21, 11, 30, 0, 0, time.UTC)

	var runtimeInputs []RuntimeContextBuildInput
	waitingRunner := &fakeRunner{
		result: RuntimeResult{
			Summary:   "await_resume",
			WaitState: RunInputRequired,
		},
		emitEvents: []RuntimeEvent{
			{
				EventType: EventRunStatus,
				Payload: map[string]any{
					"state": string(RunInputRequired),
				},
			},
		},
	}
	svc := NewService(store, bus, waitingRunner, ServiceConfig{
		Now: func() time.Time { return now },
		SessionMetadataBuilder: func(input SessionMetadataBuildInput) map[string]any {
			return map[string]any{
				"capability_set": map[string]any{
					"tools": []map[string]any{
						{"name": "file_read", "source": "native", "provider_id": "native"},
						{"name": "mcp_echo", "source": "mcp", "provider_id": "mcp.stub"},
					},
				},
			}
		},
		RuntimeContextBuilder: func(input RuntimeContextBuildInput) map[string]any {
			runtimeInputs = append(runtimeInputs, input)
			return map[string]any{
				"resolved_tool_names": []string{"file_read", "mcp_echo"},
			}
		},
	})

	firstResult, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_capability_continue",
		AgentID:       "default",
		UserMessage:   "first wait",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	if firstResult.RunID == "" {
		t.Fatal("StartTurn().RunID is empty")
	}

	secondRunner := &fakeRunner{
		result: RuntimeResult{Summary: "continued"},
	}
	svc.runner = secondRunner
	svc.now = func() time.Time { return now.Add(5 * time.Minute) }

	_, err = svc.ContinueTurn(ctx, ContinueTurnInput{
		SessionID:     "sess_capability_continue",
		AgentID:       "default",
		Message:       "continue with more detail",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("ContinueTurn() error = %v", err)
	}

	if len(runtimeInputs) != 2 {
		t.Fatalf("runtime builder input len = %d, want 2", len(runtimeInputs))
	}
	continueInput := runtimeInputs[1]
	if !continueInput.IsContinue {
		t.Fatal("continue runtime builder IsContinue = false, want true")
	}
	if continueInput.Message != "continue with more detail" {
		t.Fatalf("continue runtime builder Message = %q, want %q", continueInput.Message, "continue with more detail")
	}
	if _, ok := continueInput.SessionMetadata["capability_set"].(map[string]any); !ok {
		t.Fatalf("continue runtime builder SessionMetadata[capability_set] type = %T, want map[string]any", continueInput.SessionMetadata["capability_set"])
	}
	if names, ok := secondRunner.lastRequest.Context["resolved_tool_names"].([]string); !ok || len(names) != 2 {
		t.Fatalf("continue runner context resolved_tool_names = %#v, want [file_read mcp_echo]", secondRunner.lastRequest.Context["resolved_tool_names"])
	}
}

func TestService_ContinueTurnPassesResumeExternalTasksFromPreviousTurn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	firstRunner := &fakeRunner{
		result: RuntimeResult{
			Summary:   "await_resume",
			WaitState: RunInputRequired,
			ExternalTask: &ExternalTask{
				Adapter:         "codex",
				RemoteAgentName: "codex-cli",
				RemoteSessionID: "thread_001",
				RemoteTaskID:    "turn_remote_001",
				State:           "completed",
				Metadata: map[string]any{
					"protocol": "codex_app_server",
				},
			},
		},
		emitEvents: []RuntimeEvent{
			{
				EventType: EventRunStatus,
				Payload: map[string]any{
					"state": string(RunInputRequired),
				},
			},
		},
	}
	now := time.Date(2026, 4, 12, 11, 0, 0, 0, time.UTC)
	svc := NewService(store, bus, firstRunner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	firstResult, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_001",
		AgentID:       "default",
		UserMessage:   "delegate first turn",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	secondRunner := &fakeRunner{
		result: RuntimeResult{Summary: "continued"},
	}
	svc.runner = secondRunner
	svc.now = func() time.Time { return now.Add(5 * time.Minute) }

	_, err = svc.ContinueTurn(ctx, ContinueTurnInput{
		SessionID:     "sess_001",
		AgentID:       "default",
		Message:       "please continue with same remote thread",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("ContinueTurn() error = %v", err)
	}

	if secondRunner.lastRequest.Context == nil {
		t.Fatal("runner request Context is nil")
	}
	if secondRunner.lastRequest.Context["resume_turn_id"] != firstResult.TurnID {
		t.Fatalf("runner request Context[resume_turn_id] = %v, want %q", secondRunner.lastRequest.Context["resume_turn_id"], firstResult.TurnID)
	}
	resumeTasks, ok := secondRunner.lastRequest.Context["resume_external_tasks"].([]ExternalTask)
	if !ok {
		t.Fatalf("runner request Context[resume_external_tasks] type = %T, want []ExternalTask", secondRunner.lastRequest.Context["resume_external_tasks"])
	}
	if len(resumeTasks) != 1 {
		t.Fatalf("runner request Context[resume_external_tasks] len = %d, want 1", len(resumeTasks))
	}
	if resumeTasks[0].RemoteSessionID != "thread_001" {
		t.Fatalf("runner request Context[resume_external_tasks][0].RemoteSessionID = %q, want %q", resumeTasks[0].RemoteSessionID, "thread_001")
	}
	if secondRunner.lastRequest.Context["external_task_id"] == nil {
		t.Fatal("runner request Context[external_task_id] is nil")
	}
}

func TestService_StartTurnPublishesFailedLifecycleWhenRunnerFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		err: errors.New("runner failed"),
	}
	now := time.Date(2026, 4, 11, 11, 10, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	result, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_002",
		AgentID:       "default",
		UserMessage:   "implement broken feature",
		InitiatorType: InitiatorUser,
	})
	if err == nil {
		t.Fatal("StartTurn() error = nil, want runner failure")
	}
	if result.SessionID != "sess_002" {
		t.Fatalf("StartTurn().SessionID = %q, want %q", result.SessionID, "sess_002")
	}
	if result.TurnID == "" {
		t.Fatal("StartTurn().TurnID is empty")
	}
	if result.RunID == "" {
		t.Fatal("StartTurn().RunID is empty")
	}
	if len(bus.events) != 3 {
		t.Fatalf("published events len = %d, want 3", len(bus.events))
	}
	if bus.events[2].EventType != EventRunFailed {
		t.Fatalf("third published event type = %q, want %q", bus.events[2].EventType, EventRunFailed)
	}
}

func TestService_StartTurnPublishesRuntimeEventsFromRunner(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{
			Summary: "done",
		},
		emitEvents: []RuntimeEvent{
			{
				EventType: EventRunExternalActivity,
				Payload: map[string]any{
					"kind": "item_started",
				},
			},
			{
				EventType: EventRunExternalMessageDelta,
				Payload: map[string]any{
					"messageId":    "turn_remote_001:item_001",
					"delta":        "hello",
					"isFirstChunk": true,
				},
			},
		},
	}
	now := time.Date(2026, 4, 12, 13, 0, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	result, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_100",
		AgentID:       "default",
		UserMessage:   "emit runtime events",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	events, err := store.ListEventsBySession(ctx, "sess_100", 0)
	if err != nil {
		t.Fatalf("ListEventsBySession() error = %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("ListEventsBySession() len = %d, want 5", len(events))
	}
	if events[2].EventType != EventRunExternalActivity {
		t.Fatalf("events[2].EventType = %q, want %q", events[2].EventType, EventRunExternalActivity)
	}
	if events[2].Payload["externalTaskId"] == "" {
		t.Fatal("events[2].Payload[externalTaskId] is empty")
	}
	if events[3].EventType != EventRunExternalMessageDelta {
		t.Fatalf("events[3].EventType = %q, want %q", events[3].EventType, EventRunExternalMessageDelta)
	}
	if events[4].EventType != EventRunFinished {
		t.Fatalf("events[4].EventType = %q, want %q", events[4].EventType, EventRunFinished)
	}
	if events[4].RunID != result.RunID {
		t.Fatalf("events[4].RunID = %q, want %q", events[4].RunID, result.RunID)
	}
}

func TestService_StartTurnPersistsRuntimeWaitingStateBeforeRunFinishes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := newStatusBlockingRunner(RuntimeEvent{
		EventType: EventRunStatus,
		Payload: map[string]any{
			"state": string(RunInputRequired),
		},
	})
	now := time.Date(2026, 4, 13, 13, 0, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	startDone := make(chan error, 1)
	go func() {
		_, err := svc.StartTurn(ctx, StartTurnInput{
			SessionID:     "sess_waiting",
			AgentID:       "default",
			UserMessage:   "wait for extra input",
			InitiatorType: InitiatorUser,
		})
		startDone <- err
	}()

	runner.WaitUntilStatusPersisted(t)
	request := runner.LastRequest()

	runRow := store.db.QueryRowContext(ctx, `
		SELECT state, ended_at
		FROM runs
		WHERE run_id = ?
	`, request.RunID)
	var runState, endedAt string
	if err := runRow.Scan(&runState, &endedAt); err != nil {
		t.Fatalf("query waiting run error = %v", err)
	}
	if runState != string(RunInputRequired) {
		t.Fatalf("persisted waiting run state = %q, want %q", runState, RunInputRequired)
	}
	if endedAt != "" {
		t.Fatalf("persisted waiting run ended_at = %q, want empty", endedAt)
	}

	turnRow := store.db.QueryRowContext(ctx, `
		SELECT status, completed_at
		FROM turns
		WHERE turn_id = ?
	`, request.TurnID)
	var turnStatus, completedAt string
	if err := turnRow.Scan(&turnStatus, &completedAt); err != nil {
		t.Fatalf("query waiting turn error = %v", err)
	}
	if turnStatus != string(TurnWaiting) {
		t.Fatalf("persisted waiting turn status = %q, want %q", turnStatus, TurnWaiting)
	}
	if completedAt != "" {
		t.Fatalf("persisted waiting turn completed_at = %q, want empty", completedAt)
	}

	sessionRow := store.db.QueryRowContext(ctx, `
		SELECT status
		FROM sessions
		WHERE session_id = ?
	`, "sess_waiting")
	var sessionStatus string
	if err := sessionRow.Scan(&sessionStatus); err != nil {
		t.Fatalf("query waiting session status error = %v", err)
	}
	if sessionStatus != string(SessionWaiting) {
		t.Fatalf("persisted waiting session status = %q, want %q", sessionStatus, SessionWaiting)
	}

	events, err := store.ListEventsBySession(ctx, "sess_waiting", 0)
	if err != nil {
		t.Fatalf("ListEventsBySession() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("ListEventsBySession() len = %d, want 3", len(events))
	}
	if events[2].EventType != EventRunStatus {
		t.Fatalf("events[2].EventType = %q, want %q", events[2].EventType, EventRunStatus)
	}

	if err := svc.StopTurn(ctx, StopTurnInput{
		SessionID: "sess_waiting",
		TurnID:    request.TurnID,
		Reason:    "stop after waiting assertion",
	}); err != nil {
		t.Fatalf("StopTurn() error = %v", err)
	}

	select {
	case err := <-startDone:
		if err != nil {
			t.Fatalf("StartTurn() error after stop = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StartTurn() did not finish after stop")
	}
}

func TestService_StartTurnPersistsNonWaitingRuntimeStateBeforeRunFinishes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := newStatusBlockingRunner(RuntimeEvent{
		EventType: EventRunStatus,
		Payload: map[string]any{
			"state": string(RunThinking),
		},
	})
	now := time.Date(2026, 4, 19, 20, 0, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	startDone := make(chan StartTurnResult, 1)
	startErr := make(chan error, 1)
	go func() {
		result, err := svc.StartTurn(ctx, StartTurnInput{
			SessionID:     "sess_thinking",
			AgentID:       "default",
			UserMessage:   "keep thinking",
			InitiatorType: InitiatorUser,
		})
		if err != nil {
			startErr <- err
			return
		}
		startDone <- result
	}()

	runner.WaitUntilStatusPersisted(t)
	request := runner.LastRequest()

	runModel, err := store.GetRun(ctx, request.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if runModel.State != RunThinking {
		t.Fatalf("persisted run state = %q, want %q", runModel.State, RunThinking)
	}
	if runModel.Summary != "" {
		t.Fatalf("persisted run summary = %q, want empty", runModel.Summary)
	}
	if !runModel.EndedAt.IsZero() {
		t.Fatalf("persisted run ended_at = %q, want zero", runModel.EndedAt)
	}

	sessionModel, err := store.GetSession(ctx, "sess_thinking")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if sessionModel.Status != SessionRunning {
		t.Fatalf("persisted session status = %q, want %q", sessionModel.Status, SessionRunning)
	}

	if err := svc.StopTurn(ctx, StopTurnInput{
		SessionID: "sess_thinking",
		TurnID:    request.TurnID,
		Reason:    "stop after thinking assertion",
	}); err != nil {
		t.Fatalf("StopTurn() error = %v", err)
	}

	select {
	case err := <-startErr:
		t.Fatalf("StartTurn() error after stop = %v", err)
	case <-startDone:
	case <-time.After(2 * time.Second):
		t.Fatal("StartTurn() did not finish after stop")
	}
}

func TestService_StartTurnDoesNotFinalizeWhenRunnerReturnsWaitState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{
			Summary:   "await_resume",
			WaitState: RunInputRequired,
		},
		emitEvents: []RuntimeEvent{
			{
				EventType: EventRunStatus,
				Payload: map[string]any{
					"state": string(RunInputRequired),
				},
			},
		},
	}
	now := time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	result, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_wait_return",
		AgentID:       "default",
		UserMessage:   "pause and wait",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	events, err := store.ListEventsBySession(ctx, "sess_wait_return", 0)
	if err != nil {
		t.Fatalf("ListEventsBySession() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("ListEventsBySession() len = %d, want 3", len(events))
	}
	if events[2].EventType != EventRunStatus {
		t.Fatalf("events[2].EventType = %q, want %q", events[2].EventType, EventRunStatus)
	}

	runRow := store.db.QueryRowContext(ctx, `
		SELECT state, summary, ended_at
		FROM runs
		WHERE run_id = ?
	`, result.RunID)
	var runState, runSummary, endedAt string
	if err := runRow.Scan(&runState, &runSummary, &endedAt); err != nil {
		t.Fatalf("query waiting run error = %v", err)
	}
	if runState != string(RunInputRequired) {
		t.Fatalf("persisted run state = %q, want %q", runState, RunInputRequired)
	}
	if runSummary != "" {
		t.Fatalf("persisted run summary = %q, want empty", runSummary)
	}
	if endedAt != "" {
		t.Fatalf("persisted run ended_at = %q, want empty", endedAt)
	}

	turnRow := store.db.QueryRowContext(ctx, `
		SELECT status, completed_at
		FROM turns
		WHERE turn_id = ?
	`, result.TurnID)
	var turnStatus, completedAt string
	if err := turnRow.Scan(&turnStatus, &completedAt); err != nil {
		t.Fatalf("query waiting turn error = %v", err)
	}
	if turnStatus != string(TurnWaiting) {
		t.Fatalf("persisted turn status = %q, want %q", turnStatus, TurnWaiting)
	}
	if completedAt != "" {
		t.Fatalf("persisted turn completed_at = %q, want empty", completedAt)
	}
}

func TestService_StartTurnDoesNotAppendDuplicateFinishedEventWhenRuntimeAlreadyEmittedOne(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	now := time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)

	runner := &fakeRunner{
		result: RuntimeResult{Summary: "done"},
		emitEvents: []RuntimeEvent{
			{
				EventType: EventRunFinished,
				Payload: map[string]any{
					"summary": "done",
				},
			},
		},
	}
	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	result, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_finished_once",
		AgentID:       "default",
		UserMessage:   "finish once",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	events, err := store.ListEventsBySession(ctx, "sess_finished_once", 0)
	if err != nil {
		t.Fatalf("ListEventsBySession() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("ListEventsBySession() len = %d, want 3", len(events))
	}
	if events[2].EventType != EventRunFinished {
		t.Fatalf("events[2].EventType = %q, want %q", events[2].EventType, EventRunFinished)
	}

	finishedCount := 0
	for _, event := range events {
		if event.EventType == EventRunFinished {
			finishedCount++
		}
	}
	if finishedCount != 1 {
		t.Fatalf("finished event count = %d, want 1", finishedCount)
	}

	runModel, err := store.GetRun(ctx, result.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if runModel.State != RunCompleted {
		t.Fatalf("persisted run state = %q, want %q", runModel.State, RunCompleted)
	}
	if runModel.Summary != "done" {
		t.Fatalf("persisted run summary = %q, want %q", runModel.Summary, "done")
	}
}

func TestService_StartTurnAddsExternalTaskIDToFinishedEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	now := time.Date(2026, 4, 19, 16, 0, 0, 0, time.UTC)

	runner := &fakeRunner{
		result: RuntimeResult{
			Summary: "external work done",
			ExternalTask: &ExternalTask{
				ExternalTaskID:  "ext_complete_001",
				Adapter:         "codex",
				RemoteAgentName: "codex-cli",
				RemoteSessionID: "thread_001",
				RemoteTaskID:    "turn_remote_001",
				State:           "completed",
			},
		},
	}
	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	result, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_finish_external_id",
		AgentID:       "default",
		UserMessage:   "finish with external task",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	events, err := store.ListEventsBySession(ctx, "sess_finish_external_id", 0)
	if err != nil {
		t.Fatalf("ListEventsBySession() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("ListEventsBySession() len = %d, want 3", len(events))
	}
	if events[2].EventType != EventRunFinished {
		t.Fatalf("events[2].EventType = %q, want %q", events[2].EventType, EventRunFinished)
	}
	if events[2].RunID != result.RunID {
		t.Fatalf("events[2].RunID = %q, want %q", events[2].RunID, result.RunID)
	}
	if events[2].Payload["externalTaskId"] != "ext_complete_001" {
		t.Fatalf("events[2].Payload[externalTaskId] = %v, want %q", events[2].Payload["externalTaskId"], "ext_complete_001")
	}
}

func TestService_StartTurnAddsExternalTaskIDToFailedEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	now := time.Date(2026, 4, 19, 16, 10, 0, 0, time.UTC)

	runner := &fakeRunner{
		err: errors.New("external execution failed"),
	}
	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	_, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_failed_external_id",
		AgentID:       "default",
		UserMessage:   "fail after external dispatch",
		InitiatorType: InitiatorUser,
	})
	if err == nil {
		t.Fatal("StartTurn() error = nil, want runner failure")
	}

	events, err := store.ListEventsBySession(ctx, "sess_failed_external_id", 0)
	if err != nil {
		t.Fatalf("ListEventsBySession() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("ListEventsBySession() len = %d, want 3", len(events))
	}
	if events[2].EventType != EventRunFailed {
		t.Fatalf("events[2].EventType = %q, want %q", events[2].EventType, EventRunFailed)
	}
	if events[2].Payload["externalTaskId"] == "" {
		t.Fatal("events[2].Payload[externalTaskId] is empty")
	}
}

func TestService_StartTurnDoesNotAppendDuplicateFailedEventWhenRuntimeAlreadyEmittedOne(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	now := time.Date(2026, 4, 19, 10, 15, 0, 0, time.UTC)

	runner := &fakeRunner{
		err: fmt.Errorf("boom"),
		emitEvents: []RuntimeEvent{
			{
				EventType: EventRunFailed,
				Payload: map[string]any{
					"error": "boom",
				},
			},
		},
	}
	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	_, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_failed_once",
		AgentID:       "default",
		UserMessage:   "fail once",
		InitiatorType: InitiatorUser,
	})
	if err == nil {
		t.Fatal("StartTurn() error = nil, want runtime failure")
	}

	events, err := store.ListEventsBySession(ctx, "sess_failed_once", 0)
	if err != nil {
		t.Fatalf("ListEventsBySession() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("ListEventsBySession() len = %d, want 3", len(events))
	}
	if events[2].EventType != EventRunFailed {
		t.Fatalf("events[2].EventType = %q, want %q", events[2].EventType, EventRunFailed)
	}

	failedCount := 0
	for _, event := range events {
		if event.EventType == EventRunFailed {
			failedCount++
		}
	}
	if failedCount != 1 {
		t.Fatalf("failed event count = %d, want 1", failedCount)
	}
}

func TestService_ContinueTurnRejectsAuthRequiredWaitState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{
			Summary:   "await_approval",
			WaitState: RunAuthRequired,
		},
		emitEvents: []RuntimeEvent{
			{
				EventType: EventRunStatus,
				Payload: map[string]any{
					"state": string(RunAuthRequired),
				},
			},
		},
	}
	now := time.Date(2026, 4, 15, 11, 0, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	firstResult, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_auth_wait",
		AgentID:       "default",
		UserMessage:   "wait for approval",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	_, err = svc.ContinueTurn(ctx, ContinueTurnInput{
		SessionID:     "sess_auth_wait",
		AgentID:       "default",
		Message:       "this should be rejected",
		InitiatorType: InitiatorUser,
	})
	if err == nil {
		t.Fatal("ContinueTurn() error = nil, want auth-required rejection")
	}
	if got := err.Error(); got != "session is waiting for approval; respond via HITL instead of continue_turn" {
		t.Fatalf("ContinueTurn() error = %q, want auth-required rejection", got)
	}

	sessionModel, err := store.GetSession(ctx, "sess_auth_wait")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if sessionModel.LastTurnID != firstResult.TurnID {
		t.Fatalf("GetSession().LastTurnID = %q, want %q", sessionModel.LastTurnID, firstResult.TurnID)
	}

	events, err := store.ListEventsBySession(ctx, "sess_auth_wait", 0)
	if err != nil {
		t.Fatalf("ListEventsBySession() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("ListEventsBySession() len = %d, want 3", len(events))
	}
}

func TestService_ContinueTurnRejectsNonWaitingLatestTurn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{
			Summary: "done",
		},
	}
	now := time.Date(2026, 4, 15, 11, 30, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	firstResult, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_done_turn",
		AgentID:       "default",
		UserMessage:   "finish this turn",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	_, err = svc.ContinueTurn(ctx, ContinueTurnInput{
		SessionID:     "sess_done_turn",
		AgentID:       "default",
		Message:       "continue completed turn",
		InitiatorType: InitiatorUser,
	})
	if err == nil {
		t.Fatal("ContinueTurn() error = nil, want non-waiting rejection")
	}
	if got := err.Error(); got != "session is not waiting for continue_turn" {
		t.Fatalf("ContinueTurn() error = %q, want %q", got, "session is not waiting for continue_turn")
	}

	sessionModel, err := store.GetSession(ctx, "sess_done_turn")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if sessionModel.LastTurnID != firstResult.TurnID {
		t.Fatalf("GetSession().LastTurnID = %q, want %q", sessionModel.LastTurnID, firstResult.TurnID)
	}

	events, err := store.ListEventsBySession(ctx, "sess_done_turn", 0)
	if err != nil {
		t.Fatalf("ListEventsBySession() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("ListEventsBySession() len = %d, want 3", len(events))
	}
}

func TestService_ContinueTurnRejectsWhenResumeStateUnavailable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	now := time.Date(2026, 4, 19, 9, 0, 0, 0, time.UTC)

	waitingRunner := &fakeRunner{
		result: RuntimeResult{
			Summary:   "await_resume",
			WaitState: RunInputRequired,
		},
		emitEvents: []RuntimeEvent{
			{
				EventType: EventRunStatus,
				Payload: map[string]any{
					"state": string(RunInputRequired),
				},
			},
		},
	}
	svc := NewService(store, bus, waitingRunner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	firstResult, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_resume_missing",
		AgentID:       "default",
		UserMessage:   "need more input",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	svc.runner = &resumeStateUnavailableRunner{}
	svc.now = func() time.Time { return now.Add(2 * time.Minute) }

	_, err = svc.ContinueTurn(ctx, ContinueTurnInput{
		SessionID:     "sess_resume_missing",
		AgentID:       "default",
		Message:       "continue after restart",
		InitiatorType: InitiatorUser,
	})
	if err == nil {
		t.Fatal("ContinueTurn() error = nil, want resume-state rejection")
	}
	if got := err.Error(); got != "waiting runtime is no longer available; start a new turn or reissue the task" {
		t.Fatalf("ContinueTurn() error = %q, want resume-state rejection", got)
	}

	sessionModel, err := store.GetSession(ctx, "sess_resume_missing")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if sessionModel.LastTurnID != firstResult.TurnID {
		t.Fatalf("GetSession().LastTurnID = %q, want %q", sessionModel.LastTurnID, firstResult.TurnID)
	}

	events, err := store.ListEventsBySession(ctx, "sess_resume_missing", 0)
	if err != nil {
		t.Fatalf("ListEventsBySession() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("ListEventsBySession() len = %d, want 3", len(events))
	}
}

func TestService_StartTurnPassesSessionTurnRunContextIntoHITL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	hitlMgr := hitl.NewInMemoryManager(nil)
	runner := &hitlTriggerRunner{manager: hitlMgr}
	now := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	resultCh := make(chan StartTurnResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := svc.StartTurn(ctx, StartTurnInput{
			SessionID:     "sess_hitl",
			AgentID:       "default",
			UserMessage:   "run with hitl context",
			InitiatorType: InitiatorUser,
		})
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()

	var ticket *hitl.Ticket
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pending := hitlMgr.ListPending()
		if len(pending) > 0 {
			ticket = pending[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ticket == nil {
		t.Fatal("HITL ticket was never created")
	}

	if ticket.SessionID != "sess_hitl" {
		t.Fatalf("ticket.SessionID = %q, want %q", ticket.SessionID, "sess_hitl")
	}
	if ticket.TurnID == "" {
		t.Fatal("ticket.TurnID is empty")
	}
	if ticket.RunID == "" {
		t.Fatal("ticket.RunID is empty")
	}

	if err := hitlMgr.Respond(ticket.ID, "continue with context"); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}

	select {
	case err := <-errCh:
		t.Fatalf("StartTurn() error = %v", err)
	case result := <-resultCh:
		if result.TurnID != ticket.TurnID {
			t.Fatalf("StartTurn().TurnID = %q, want %q", result.TurnID, ticket.TurnID)
		}
		if result.RunID != ticket.RunID {
			t.Fatalf("StartTurn().RunID = %q, want %q", result.RunID, ticket.RunID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StartTurn() did not return after HITL response")
	}
}

func TestService_StopTurnCancelsActiveRunAndPublishesCanceledLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := newBlockingRunner()
	now := time.Date(2026, 4, 13, 8, 0, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	startDone := make(chan error, 1)
	go func() {
		_, err := svc.StartTurn(ctx, StartTurnInput{
			SessionID:     "sess_stop",
			AgentID:       "default",
			UserMessage:   "run until stopped",
			InitiatorType: InitiatorUser,
		})
		startDone <- err
	}()

	runner.WaitUntilRunning(t)

	if err := svc.StopTurn(ctx, StopTurnInput{
		SessionID: "sess_stop",
		TurnID:    runner.LastRequest().TurnID,
		Reason:    "stopped by operator",
	}); err != nil {
		t.Fatalf("StopTurn() error = %v", err)
	}

	select {
	case err := <-startDone:
		if err != nil {
			t.Fatalf("StartTurn() error after stop = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StartTurn() did not return after stop")
	}

	events, err := store.ListEventsBySession(ctx, "sess_stop", 0)
	if err != nil {
		t.Fatalf("ListEventsBySession() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("ListEventsBySession() len = %d, want 3", len(events))
	}
	if events[2].EventType != EventRunCanceled {
		t.Fatalf("events[2].EventType = %q, want %q", events[2].EventType, EventRunCanceled)
	}
	if events[2].Payload["reason"] != "stopped by operator" {
		t.Fatalf("events[2].Payload[reason] = %v, want %q", events[2].Payload["reason"], "stopped by operator")
	}

	row := store.db.QueryRowContext(ctx, `
		SELECT state, ended_at
		FROM runs
		WHERE run_id = ?
	`, runner.LastRequest().RunID)
	var runState, endedAt string
	if err := row.Scan(&runState, &endedAt); err != nil {
		t.Fatalf("query canceled run error = %v", err)
	}
	if runState != string(RunCanceled) {
		t.Fatalf("persisted run state = %q, want %q", runState, RunCanceled)
	}
	if endedAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("persisted run ended_at = %q, want %q", endedAt, now.Format(time.RFC3339Nano))
	}

	turnRow := store.db.QueryRowContext(ctx, `
		SELECT status, completed_at
		FROM turns
		WHERE turn_id = ?
	`, runner.LastRequest().TurnID)
	var turnStatus, completedAt string
	if err := turnRow.Scan(&turnStatus, &completedAt); err != nil {
		t.Fatalf("query canceled turn error = %v", err)
	}
	if turnStatus != string(TurnCanceled) {
		t.Fatalf("persisted turn status = %q, want %q", turnStatus, TurnCanceled)
	}
	if completedAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("persisted turn completed_at = %q, want %q", completedAt, now.Format(time.RFC3339Nano))
	}
}

func TestService_StartTurnPersistsCompletedLifecycleState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{
			Summary: "final summary",
			ExternalTask: &ExternalTask{
				ExternalTaskID:  "ext_complete_001",
				Adapter:         "codex",
				RemoteAgentName: "codex-cli",
				RemoteSessionID: "remote_sess_001",
				RemoteTaskID:    "remote_task_001",
				State:           "completed",
				ResultSummary:   "remote task done",
			},
		},
	}
	now := time.Date(2026, 4, 13, 9, 0, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	result, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_done",
		AgentID:       "default",
		UserMessage:   "finish the task",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	row := store.db.QueryRowContext(ctx, `
		SELECT state, summary, ended_at
		FROM runs
		WHERE run_id = ?
	`, result.RunID)
	var runState, runSummary, runEndedAt string
	if err := row.Scan(&runState, &runSummary, &runEndedAt); err != nil {
		t.Fatalf("query run state error = %v", err)
	}
	if runState != string(RunCompleted) {
		t.Fatalf("persisted run state = %q, want %q", runState, RunCompleted)
	}
	if runSummary != "final summary" {
		t.Fatalf("persisted run summary = %q, want %q", runSummary, "final summary")
	}
	if runEndedAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("persisted run ended_at = %q, want %q", runEndedAt, now.Format(time.RFC3339Nano))
	}

	sessionRow := store.db.QueryRowContext(ctx, `
		SELECT status
		FROM sessions
		WHERE session_id = ?
	`, result.SessionID)
	var sessionStatus string
	if err := sessionRow.Scan(&sessionStatus); err != nil {
		t.Fatalf("query session status error = %v", err)
	}
	if sessionStatus != string(SessionOpen) {
		t.Fatalf("persisted session status = %q, want %q", sessionStatus, SessionOpen)
	}

	taskRow := store.db.QueryRowContext(ctx, `
		SELECT state, result_summary
		FROM external_tasks
		WHERE external_task_id = ?
	`, "ext_complete_001")
	var taskState, taskSummary string
	if err := taskRow.Scan(&taskState, &taskSummary); err != nil {
		t.Fatalf("query external task error = %v", err)
	}
	if taskState != "completed" {
		t.Fatalf("persisted external task state = %q, want %q", taskState, "completed")
	}
	if taskSummary != "remote task done" {
		t.Fatalf("persisted external task result summary = %q, want %q", taskSummary, "remote task done")
	}
}

func TestService_StartTurnRunsRecapHookForCompletedTurn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{
			Summary: "final summary",
		},
	}
	recap := &recapRecorder{}
	now := time.Date(2026, 4, 13, 9, 15, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now:        func() time.Time { return now },
		RecapHooks: []RecapHook{recap},
	})

	result, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_recap_done",
		AgentID:       "default",
		UserMessage:   "finish with recap",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	if len(recap.inputs) != 1 {
		t.Fatalf("recap inputs len = %d, want 1", len(recap.inputs))
	}
	if recap.inputs[0].Phase != RecapPhaseCompleted {
		t.Fatalf("recap phase = %q, want %q", recap.inputs[0].Phase, RecapPhaseCompleted)
	}
	if recap.inputs[0].TurnID != result.TurnID {
		t.Fatalf("recap turn id = %q, want %q", recap.inputs[0].TurnID, result.TurnID)
	}
	if recap.inputs[0].RunID != result.RunID {
		t.Fatalf("recap run id = %q, want %q", recap.inputs[0].RunID, result.RunID)
	}
	if recap.inputs[0].Summary != "final summary" {
		t.Fatalf("recap summary = %q, want %q", recap.inputs[0].Summary, "final summary")
	}
}

func TestService_StartTurnRunsRecapHookForFailedTurn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		err: errors.New("runner failed"),
	}
	recap := &recapRecorder{}
	now := time.Date(2026, 4, 13, 9, 20, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now:        func() time.Time { return now },
		RecapHooks: []RecapHook{recap},
	})

	_, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_recap_failed",
		AgentID:       "default",
		UserMessage:   "fail with recap",
		InitiatorType: InitiatorUser,
	})
	if err == nil {
		t.Fatal("StartTurn() error = nil, want failure")
	}

	if len(recap.inputs) != 1 {
		t.Fatalf("recap inputs len = %d, want 1", len(recap.inputs))
	}
	if recap.inputs[0].Phase != RecapPhaseFailed {
		t.Fatalf("recap phase = %q, want %q", recap.inputs[0].Phase, RecapPhaseFailed)
	}
	if recap.inputs[0].Failure != "runner failed" {
		t.Fatalf("recap failure = %q, want %q", recap.inputs[0].Failure, "runner failed")
	}
}

func TestService_StartTurnRunsRecapHookForCanceledTurn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := newBlockingRunner()
	recap := &recapRecorder{}
	now := time.Date(2026, 4, 13, 9, 25, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now:        func() time.Time { return now },
		RecapHooks: []RecapHook{recap},
	})

	startDone := make(chan error, 1)
	go func() {
		_, err := svc.StartTurn(ctx, StartTurnInput{
			SessionID:     "sess_recap_cancel",
			AgentID:       "default",
			UserMessage:   "cancel with recap",
			InitiatorType: InitiatorUser,
		})
		startDone <- err
	}()

	runner.WaitUntilRunning(t)

	if err := svc.StopTurn(ctx, StopTurnInput{
		SessionID: "sess_recap_cancel",
		TurnID:    runner.LastRequest().TurnID,
		Reason:    "operator canceled",
	}); err != nil {
		t.Fatalf("StopTurn() error = %v", err)
	}

	select {
	case err := <-startDone:
		if err != nil {
			t.Fatalf("StartTurn() error after stop = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StartTurn() did not finish after stop")
	}

	if len(recap.inputs) != 1 {
		t.Fatalf("recap inputs len = %d, want 1", len(recap.inputs))
	}
	if recap.inputs[0].Phase != RecapPhaseCanceled {
		t.Fatalf("recap phase = %q, want %q", recap.inputs[0].Phase, RecapPhaseCanceled)
	}
	if recap.inputs[0].Failure != "operator canceled" {
		t.Fatalf("recap failure = %q, want %q", recap.inputs[0].Failure, "operator canceled")
	}
}

func TestService_StartTurnWaitingStateDoesNotRunRecapHook(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{
			Summary:   "await_resume",
			WaitState: RunInputRequired,
		},
		emitEvents: []RuntimeEvent{
			{
				EventType: EventRunStatus,
				Payload: map[string]any{
					"state": string(RunInputRequired),
				},
			},
		},
	}
	recap := &recapRecorder{}
	now := time.Date(2026, 4, 13, 9, 30, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now:        func() time.Time { return now },
		RecapHooks: []RecapHook{recap},
	})

	_, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_recap_wait",
		AgentID:       "default",
		UserMessage:   "wait without recap",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	if len(recap.inputs) != 0 {
		t.Fatalf("recap inputs len = %d, want 0", len(recap.inputs))
	}
}

func TestService_StartTurnIgnoresRecapHookError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{
			Summary: "final summary",
		},
	}
	recap := &recapRecorder{err: errors.New("recap sink failed")}
	now := time.Date(2026, 4, 13, 9, 35, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now:        func() time.Time { return now },
		RecapHooks: []RecapHook{recap},
	})

	result, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_recap_best_effort",
		AgentID:       "default",
		UserMessage:   "finish despite recap error",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	if result.RunID == "" {
		t.Fatal("StartTurn().RunID is empty")
	}
	if len(recap.inputs) != 1 {
		t.Fatalf("recap inputs len = %d, want 1", len(recap.inputs))
	}
}

func TestService_StartTurnRunsPostTurnActionHookForCompletedTurn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{
			Summary: "final summary",
		},
	}
	post := &postTurnActionRecorder{}
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now:                 func() time.Time { return now },
		PostTurnActionHooks: []PostTurnActionHook{post},
	})

	result, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_post_completed",
		AgentID:       "default",
		UserMessage:   "finish with post turn hook",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	if len(post.inputs) != 1 {
		t.Fatalf("post turn inputs len = %d, want 1", len(post.inputs))
	}
	if post.inputs[0].Outcome != TurnOutcomeCompleted {
		t.Fatalf("post turn outcome = %q, want %q", post.inputs[0].Outcome, TurnOutcomeCompleted)
	}
	if post.inputs[0].RunID != result.RunID {
		t.Fatalf("post turn run id = %q, want %q", post.inputs[0].RunID, result.RunID)
	}
	if post.inputs[0].Summary != "final summary" {
		t.Fatalf("post turn summary = %q, want %q", post.inputs[0].Summary, "final summary")
	}
	if post.inputs[0].LoopMetadata["loop_phase"] != "initial" {
		t.Fatalf("post turn loop metadata loop_phase = %v, want %q", post.inputs[0].LoopMetadata["loop_phase"], "initial")
	}
}

func TestService_StartTurnPersistsPostTurnPlannerHints(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{
			Summary: "final summary",
		},
	}
	post := &postTurnActionRecorder{
		result: &PostTurnActionResult{
			PlannerHint: &PlannerHint{
				Stage:   "review",
				Summary: "review the last turn",
				Metadata: map[string]any{
					"source": "hook",
				},
			},
			NextActionHint: &NextActionHint{
				Kind:    "review",
				Title:   "Review Needed",
				Message: "Please inspect the completed turn.",
				Metadata: map[string]any{
					"priority": "high",
				},
			},
		},
	}
	now := time.Date(2026, 4, 20, 12, 2, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now:                 func() time.Time { return now },
		PostTurnActionHooks: []PostTurnActionHook{post},
	})

	result, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_post_hints",
		AgentID:       "default",
		UserMessage:   "finish with hints",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	runModel, err := store.GetRun(ctx, result.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	plannerHint, ok := runModel.Metadata["planner_hint"].(map[string]any)
	if !ok {
		t.Fatalf("run metadata planner_hint type = %T, want map[string]any", runModel.Metadata["planner_hint"])
	}
	if plannerHint["stage"] != "review" {
		t.Fatalf("run metadata planner_hint.stage = %v, want %q", plannerHint["stage"], "review")
	}
	nextActionHint, ok := runModel.Metadata["next_action_hint"].(map[string]any)
	if !ok {
		t.Fatalf("run metadata next_action_hint type = %T, want map[string]any", runModel.Metadata["next_action_hint"])
	}
	if nextActionHint["kind"] != "review" {
		t.Fatalf("run metadata next_action_hint.kind = %v, want %q", nextActionHint["kind"], "review")
	}
	if nextActionHint["message"] != "Please inspect the completed turn." {
		t.Fatalf("run metadata next_action_hint.message = %v, want message", nextActionHint["message"])
	}
}

func TestService_StartTurnWaitingStateDoesNotRunPostTurnActionHook(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{
			Summary:   "await_resume",
			WaitState: RunInputRequired,
		},
		emitEvents: []RuntimeEvent{
			{
				EventType: EventRunStatus,
				Payload: map[string]any{
					"state": string(RunInputRequired),
				},
			},
		},
	}
	post := &postTurnActionRecorder{}
	now := time.Date(2026, 4, 20, 12, 5, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now:                 func() time.Time { return now },
		PostTurnActionHooks: []PostTurnActionHook{post},
	})

	_, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_post_waiting",
		AgentID:       "default",
		UserMessage:   "wait without post turn hook",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	if len(post.inputs) != 0 {
		t.Fatalf("post turn inputs len = %d, want 0", len(post.inputs))
	}
}

func TestService_StartTurnIgnoresPostTurnActionHookError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{
			Summary: "final summary",
		},
	}
	post := &postTurnActionRecorder{err: errors.New("post turn action failed")}
	now := time.Date(2026, 4, 20, 12, 10, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now:                 func() time.Time { return now },
		PostTurnActionHooks: []PostTurnActionHook{post},
	})

	result, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_post_best_effort",
		AgentID:       "default",
		UserMessage:   "finish despite post turn error",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	if result.RunID == "" {
		t.Fatal("StartTurn().RunID is empty")
	}
	if len(post.inputs) != 1 {
		t.Fatalf("post turn inputs len = %d, want 1", len(post.inputs))
	}
}

func TestService_StartTurnStructuredRecapHookPersistsRecap(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{
			Summary: "final summary",
		},
	}
	now := time.Date(2026, 4, 16, 10, 30, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
		RecapHooks: []RecapHook{
			NewStructuredRecapHook(store, func() time.Time { return now }),
		},
	})

	result, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_structured_recap",
		AgentID:       "default",
		UserMessage:   "finish with structured recap",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	recaps, err := store.ListRecapsByTurn(ctx, "sess_structured_recap", result.TurnID)
	if err != nil {
		t.Fatalf("ListRecapsByTurn() error = %v", err)
	}
	if len(recaps) != 1 {
		t.Fatalf("ListRecapsByTurn() len = %d, want 1", len(recaps))
	}
	if recaps[0].RunID != result.RunID {
		t.Fatalf("ListRecapsByTurn()[0].RunID = %q, want %q", recaps[0].RunID, result.RunID)
	}
	if recaps[0].Content != "final summary" {
		t.Fatalf("ListRecapsByTurn()[0].Content = %q, want %q", recaps[0].Content, "final summary")
	}
	if recaps[0].Metadata["phase"] != string(RecapPhaseCompleted) {
		t.Fatalf("ListRecapsByTurn()[0].Metadata[phase] = %v, want %q", recaps[0].Metadata["phase"], RecapPhaseCompleted)
	}
}

func TestService_StartTurnRecapHookPersistsMemoryCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{
			Summary: "Need to keep wait/resume rules stable and user prefers concise host status feedback.",
		},
	}
	now := time.Date(2026, 4, 16, 11, 30, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
		RecapHooks: []RecapHook{
			NewStructuredRecapHook(store, func() time.Time { return now }),
			NewMemoryCandidateHook(store, func() time.Time { return now }),
		},
	})

	result, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_memory_candidates",
		AgentID:       "default",
		UserMessage:   "finish with memory candidates",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	candidates, err := store.ListMemoryCandidatesByTurn(ctx, "sess_memory_candidates", result.TurnID)
	if err != nil {
		t.Fatalf("ListMemoryCandidatesByTurn() error = %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("ListMemoryCandidatesByTurn() len = %d, want 2", len(candidates))
	}
	if candidates[0].SourceRecapID == "" {
		t.Fatal("ListMemoryCandidatesByTurn()[0].SourceRecapID is empty")
	}
	if candidates[0].Status != MemoryCandidateDraft {
		t.Fatalf("ListMemoryCandidatesByTurn()[0].Status = %q, want %q", candidates[0].Status, MemoryCandidateDraft)
	}
	if candidates[0].Metadata["phase"] != string(RecapPhaseCompleted) {
		t.Fatalf("ListMemoryCandidatesByTurn()[0].Metadata[phase] = %v, want %q", candidates[0].Metadata["phase"], RecapPhaseCompleted)
	}

	foundMemory := false
	foundUser := false
	for _, candidate := range candidates {
		if candidate.Target == "MEMORY" {
			foundMemory = true
		}
		if candidate.Target == "USER" {
			foundUser = true
		}
	}
	if !foundMemory {
		t.Fatal("expected MEMORY candidate")
	}
	if !foundUser {
		t.Fatal("expected USER candidate")
	}
}

func TestService_UpdateMemoryCandidateAcceptsAndRejects(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	root := t.TempDir()
	now := time.Date(2026, 4, 16, 16, 0, 0, 0, time.UTC)
	svc := NewService(store, nil, nil, ServiceConfig{
		MemoryFileSync: NewMemoryFileWriter(store, root, func() time.Time { return now }),
	})

	if err := store.CreateMemoryCandidate(ctx, MemoryCandidate{
		CandidateID:   "mcand_service_001",
		SessionID:     "sess_001",
		TurnID:        "turn_001",
		RunID:         "run_001",
		SourceRecapID: "recap_001",
		Target:        "MEMORY",
		Kind:          "turn_learning",
		Content:       "candidate for action",
		Score:         0.8,
		Status:        MemoryCandidateDraft,
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("CreateMemoryCandidate() error = %v", err)
	}

	applied, err := svc.UpdateMemoryCandidate(ctx, UpdateMemoryCandidateInput{
		CandidateID: "mcand_service_001",
		Status:      MemoryCandidateApplied,
	})
	if err != nil {
		t.Fatalf("UpdateMemoryCandidate(applied) error = %v", err)
	}
	if applied.Status != MemoryCandidateApplied {
		t.Fatalf("UpdateMemoryCandidate(applied).Status = %q, want %q", applied.Status, MemoryCandidateApplied)
	}
	memoryFormal, err := os.ReadFile(filepath.Join(root, "memory", "MEMORY.md"))
	if err != nil {
		t.Fatalf("os.ReadFile(MEMORY.md) after accept error = %v", err)
	}
	if !strings.Contains(string(memoryFormal), "candidate for action") {
		t.Fatalf("MEMORY.md after accept = %q, want applied candidate content", string(memoryFormal))
	}

	rejected, err := svc.UpdateMemoryCandidate(ctx, UpdateMemoryCandidateInput{
		CandidateID: "mcand_service_001",
		Status:      MemoryCandidateRejected,
	})
	if err != nil {
		t.Fatalf("UpdateMemoryCandidate(rejected) error = %v", err)
	}
	if rejected.Status != MemoryCandidateRejected {
		t.Fatalf("UpdateMemoryCandidate(rejected).Status = %q, want %q", rejected.Status, MemoryCandidateRejected)
	}
	memoryFormal, err = os.ReadFile(filepath.Join(root, "memory", "MEMORY.md"))
	if err != nil {
		t.Fatalf("os.ReadFile(MEMORY.md) after reject error = %v", err)
	}
	if strings.Contains(string(memoryFormal), "candidate for action") {
		t.Fatalf("MEMORY.md after reject = %q, should not include rejected candidate", string(memoryFormal))
	}
}

func TestService_UpdateMemoryCandidateRejectsUnsupportedStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	svc := NewService(store, nil, nil, ServiceConfig{})

	_, err := svc.UpdateMemoryCandidate(ctx, UpdateMemoryCandidateInput{
		CandidateID: "mcand_service_invalid",
		Status:      MemoryCandidateRendered,
	})
	if err == nil {
		t.Fatal("UpdateMemoryCandidate() error = nil, want validation error")
	}
	if got := err.Error(); got != "memory candidate status must be applied or rejected" {
		t.Fatalf("UpdateMemoryCandidate() error = %q, want validation error", got)
	}
}

func TestService_StartTurnPersistsFailedLifecycleState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		err: errors.New("runner failed"),
	}
	now := time.Date(2026, 4, 13, 9, 30, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	_, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_failed",
		AgentID:       "default",
		UserMessage:   "break the task",
		InitiatorType: InitiatorUser,
	})
	if err == nil {
		t.Fatal("StartTurn() error = nil, want runner failure")
	}

	row := store.db.QueryRowContext(ctx, `
		SELECT run_id, state, summary, ended_at
		FROM runs
		WHERE session_id = ?
		ORDER BY started_at DESC
		LIMIT 1
	`, "sess_failed")
	var runID, runState, runSummary, runEndedAt string
	if err := row.Scan(&runID, &runState, &runSummary, &runEndedAt); err != nil {
		t.Fatalf("query failed run state error = %v", err)
	}
	if runID == "" {
		t.Fatal("persisted failed run id is empty")
	}
	if runState != string(RunFailed) {
		t.Fatalf("persisted failed run state = %q, want %q", runState, RunFailed)
	}
	if runSummary != "" {
		t.Fatalf("persisted failed run summary = %q, want empty", runSummary)
	}
	if runEndedAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("persisted failed run ended_at = %q, want %q", runEndedAt, now.Format(time.RFC3339Nano))
	}

	sessionRow := store.db.QueryRowContext(ctx, `
		SELECT status
		FROM sessions
		WHERE session_id = ?
	`, "sess_failed")
	var sessionStatus string
	if err := sessionRow.Scan(&sessionStatus); err != nil {
		t.Fatalf("query failed session status error = %v", err)
	}
	if sessionStatus != string(SessionOpen) {
		t.Fatalf("persisted failed session status = %q, want %q", sessionStatus, SessionOpen)
	}
}

func TestService_ContinueTurnKeepsSessionEventSequenceIncreasingAcrossTurns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	now := time.Date(2026, 4, 14, 14, 0, 0, 0, time.UTC)

	waitingRunner := &fakeRunner{
		result: RuntimeResult{
			Summary:   "await_resume",
			WaitState: RunInputRequired,
		},
		emitEvents: []RuntimeEvent{
			{
				EventType: EventRunStatus,
				Payload: map[string]any{
					"state": string(RunInputRequired),
				},
			},
		},
	}
	svc := NewService(store, bus, waitingRunner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	firstResult, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_sequence",
		AgentID:       "default",
		UserMessage:   "first turn",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	completingRunner := &fakeRunner{
		result: RuntimeResult{Summary: "done"},
	}
	svc.runner = completingRunner
	svc.now = func() time.Time { return now.Add(2 * time.Minute) }
	secondResult, err := svc.ContinueTurn(ctx, ContinueTurnInput{
		SessionID:     "sess_sequence",
		AgentID:       "default",
		Message:       "second turn",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("ContinueTurn() error = %v", err)
	}

	events, err := store.ListEventsBySession(ctx, "sess_sequence", 0)
	if err != nil {
		t.Fatalf("ListEventsBySession() error = %v", err)
	}
	if len(events) != 6 {
		t.Fatalf("ListEventsBySession() len = %d, want 6", len(events))
	}

	for idx, event := range events {
		want := int64(idx + 1)
		if event.Sequence != want {
			t.Fatalf("events[%d].Sequence = %d, want %d", idx, event.Sequence, want)
		}
	}
	if events[2].RunID != firstResult.RunID {
		t.Fatalf("events[2].RunID = %q, want %q", events[2].RunID, firstResult.RunID)
	}
	if events[3].RunID != secondResult.RunID {
		t.Fatalf("events[3].RunID = %q, want %q", events[3].RunID, secondResult.RunID)
	}
	if bus.events[5].Sequence != 6 {
		t.Fatalf("bus.events[5].Sequence = %d, want %d", bus.events[5].Sequence, 6)
	}

	firstRun, err := store.GetRun(ctx, firstResult.RunID)
	if err != nil {
		t.Fatalf("GetRun(first) error = %v", err)
	}
	if firstRun.ResumeFromRunID != "" {
		t.Fatalf("first run ResumeFromRunID = %q, want empty", firstRun.ResumeFromRunID)
	}

	secondRun, err := store.GetRun(ctx, secondResult.RunID)
	if err != nil {
		t.Fatalf("GetRun(second) error = %v", err)
	}
	if secondRun.ResumeFromRunID != firstResult.RunID {
		t.Fatalf("second run ResumeFromRunID = %q, want %q", secondRun.ResumeFromRunID, firstResult.RunID)
	}
	if secondRun.TurnID != secondResult.TurnID {
		t.Fatalf("second run TurnID = %q, want %q", secondRun.TurnID, secondResult.TurnID)
	}
}

func TestService_ContinueTurnStoresResumeFromLatestWaitingRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	now := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)

	waitingRunner := &fakeRunner{
		result: RuntimeResult{
			Summary:   "await_resume",
			WaitState: RunInputRequired,
		},
		emitEvents: []RuntimeEvent{
			{
				EventType: EventRunStatus,
				Payload: map[string]any{
					"state": string(RunInputRequired),
				},
			},
		},
	}
	svc := NewService(store, bus, waitingRunner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	firstResult, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_resume_from",
		AgentID:       "default",
		UserMessage:   "first turn",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	svc.runner = &fakeRunner{
		result: RuntimeResult{Summary: "done"},
	}
	svc.now = func() time.Time { return now.Add(1 * time.Minute) }

	secondResult, err := svc.ContinueTurn(ctx, ContinueTurnInput{
		SessionID:     "sess_resume_from",
		AgentID:       "default",
		Message:       "continue turn",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("ContinueTurn() error = %v", err)
	}

	firstRun, err := store.GetRun(ctx, firstResult.RunID)
	if err != nil {
		t.Fatalf("GetRun(first) error = %v", err)
	}
	if firstRun.ResumeFromRunID != "" {
		t.Fatalf("first run ResumeFromRunID = %q, want empty", firstRun.ResumeFromRunID)
	}

	secondRun, err := store.GetRun(ctx, secondResult.RunID)
	if err != nil {
		t.Fatalf("GetRun(second) error = %v", err)
	}
	if secondRun.ResumeFromRunID != firstResult.RunID {
		t.Fatalf("second run ResumeFromRunID = %q, want %q", secondRun.ResumeFromRunID, firstResult.RunID)
	}
	if secondRun.SessionID != "sess_resume_from" {
		t.Fatalf("second run SessionID = %q, want %q", secondRun.SessionID, "sess_resume_from")
	}
}

type fakeRunner struct {
	result      RuntimeResult
	err         error
	lastRequest RuntimeRequest
	emitEvents  []RuntimeEvent
}

func (f *fakeRunner) Run(_ context.Context, in RuntimeRequest) (RuntimeResult, error) {
	f.lastRequest = in
	for _, event := range f.emitEvents {
		if in.Emit == nil {
			continue
		}
		if err := in.Emit(event); err != nil {
			return RuntimeResult{}, err
		}
	}
	return f.result, f.err
}

type resumeStateUnavailableRunner struct{}

func (r *resumeStateUnavailableRunner) Run(context.Context, RuntimeRequest) (RuntimeResult, error) {
	return RuntimeResult{}, nil
}

func (r *resumeStateUnavailableRunner) ValidateContinue(context.Context, RuntimeRequest) error {
	return fmt.Errorf("waiting runtime is no longer available; start a new turn or reissue the task")
}

type recapRecorder struct {
	inputs []RecapInput
	err    error
}

func (r *recapRecorder) OnTurnRecap(_ context.Context, input RecapInput) error {
	r.inputs = append(r.inputs, input)
	return r.err
}

type postTurnActionRecorder struct {
	inputs []PostTurnActionInput
	err    error
	result *PostTurnActionResult
}

func (r *postTurnActionRecorder) OnPostTurnAction(_ context.Context, input PostTurnActionInput) (*PostTurnActionResult, error) {
	r.inputs = append(r.inputs, input)
	return r.result, r.err
}

type blockingRunner struct {
	mu          sync.Mutex
	lastRequest RuntimeRequest
	startedCh   chan struct{}
}

func newBlockingRunner() *blockingRunner {
	return &blockingRunner{
		startedCh: make(chan struct{}),
	}
}

func (r *blockingRunner) Run(ctx context.Context, in RuntimeRequest) (RuntimeResult, error) {
	r.mu.Lock()
	r.lastRequest = in
	r.mu.Unlock()

	select {
	case <-r.startedCh:
	default:
		close(r.startedCh)
	}

	<-ctx.Done()
	return RuntimeResult{}, ctx.Err()
}

func (r *blockingRunner) WaitUntilRunning(t *testing.T) {
	t.Helper()

	select {
	case <-r.startedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("blocking runner did not start")
	}
}

func (r *blockingRunner) LastRequest() RuntimeRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastRequest
}

type statusBlockingRunner struct {
	mu          sync.Mutex
	lastRequest RuntimeRequest
	statusEvent RuntimeEvent
	startedCh   chan struct{}
	emittedCh   chan struct{}
}

func newStatusBlockingRunner(statusEvent RuntimeEvent) *statusBlockingRunner {
	return &statusBlockingRunner{
		statusEvent: statusEvent,
		startedCh:   make(chan struct{}),
		emittedCh:   make(chan struct{}),
	}
}

func (r *statusBlockingRunner) Run(ctx context.Context, in RuntimeRequest) (RuntimeResult, error) {
	r.mu.Lock()
	r.lastRequest = in
	r.mu.Unlock()

	select {
	case <-r.startedCh:
	default:
		close(r.startedCh)
	}

	if in.Emit != nil {
		if err := in.Emit(r.statusEvent); err != nil {
			return RuntimeResult{}, err
		}
	}
	select {
	case <-r.emittedCh:
	default:
		close(r.emittedCh)
	}

	<-ctx.Done()
	return RuntimeResult{}, ctx.Err()
}

func (r *statusBlockingRunner) WaitUntilStatusPersisted(t *testing.T) {
	t.Helper()

	select {
	case <-r.emittedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("status blocking runner did not emit status")
	}
}

func (r *statusBlockingRunner) LastRequest() RuntimeRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastRequest
}

type hitlTriggerRunner struct {
	manager hitl.Manager
}

func (r *hitlTriggerRunner) Run(ctx context.Context, in RuntimeRequest) (RuntimeResult, error) {
	if r == nil || r.manager == nil {
		return RuntimeResult{}, errors.New("hitl manager is required")
	}

	resp, err := r.manager.RequestIntervention(
		ctx,
		in.AgentID,
		"task_001",
		"need human confirmation",
		"tool execution needs approval",
	)
	if err != nil {
		return RuntimeResult{}, err
	}

	return RuntimeResult{
		Summary: resp.Instruction,
	}, nil
}

type fakePublisher struct {
	events []RunEvent
}

func newFakePublisher() *fakePublisher {
	return &fakePublisher{events: make([]RunEvent, 0, 4)}
}

func (f *fakePublisher) PublishSessionEvent(_ context.Context, event RunEvent) error {
	f.events = append(f.events, event)
	return nil
}

func newServiceTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := createMigratedTestDB(t)
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return store
}
