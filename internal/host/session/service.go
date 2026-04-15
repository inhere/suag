package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/inhere/axon/internal/ops/hitl"
)

var ErrSessionNotFound = errors.New("session not found")

type Runner interface {
	Run(ctx context.Context, in RuntimeRequest) (RuntimeResult, error)
}

type Publisher interface {
	PublishSessionEvent(ctx context.Context, event RunEvent) error
}

type RuntimeRequest struct {
	SessionID   string
	TurnID      string
	RunID       string
	AgentID     string
	Instruction string
	Context     map[string]any
	Emit        func(event RuntimeEvent) error
}

type RuntimeResult struct {
	Summary      string
	WaitState    RunState
	ExternalTask *ExternalTask
}

type RuntimeEvent struct {
	EventType string
	Payload   map[string]any
}

type ServiceConfig struct {
	Now func() time.Time
}

type StartTurnInput struct {
	SessionID     string
	AgentID       string
	UserMessage   string
	InitiatorType InitiatorType
}

type ContinueTurnInput struct {
	SessionID     string
	AgentID       string
	Message       string
	InitiatorType InitiatorType
}

type StopTurnInput struct {
	SessionID string
	TurnID    string
	Reason    string
}

type StartTurnResult struct {
	SessionID string
	TurnID    string
	RunID     string
}

type activeExecution struct {
	sessionID string
	turnID    string
	runID     string
	agentID   string
	cancel    context.CancelFunc
	stopped   bool
	reason    string
}

// Service owns the host-side lifecycle orchestration for session turns and runs.
type Service struct {
	store  Store
	pub    Publisher
	runner Runner
	now    func() time.Time

	activeMu         sync.Mutex
	activeExecutions map[string]*activeExecution
}

func NewService(store Store, pub Publisher, runner Runner, cfg ServiceConfig) *Service {
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	return &Service{
		store:            store,
		pub:              pub,
		runner:           runner,
		now:              nowFn,
		activeExecutions: make(map[string]*activeExecution),
	}
}

func (s *Service) StartTurn(ctx context.Context, in StartTurnInput) (StartTurnResult, error) {
	if s == nil {
		return StartTurnResult{}, fmt.Errorf("service is required")
	}
	if err := validateStartTurnInput(in); err != nil {
		return StartTurnResult{}, err
	}
	return s.startTurn(ctx, in.SessionID, in.AgentID, in.UserMessage, in.InitiatorType, true)
}

func (s *Service) ContinueTurn(ctx context.Context, in ContinueTurnInput) (StartTurnResult, error) {
	if s == nil {
		return StartTurnResult{}, fmt.Errorf("service is required")
	}
	if err := validateContinueTurnInput(in); err != nil {
		return StartTurnResult{}, err
	}
	return s.startTurn(ctx, in.SessionID, in.AgentID, in.Message, in.InitiatorType, false)
}

func (s *Service) StopTurn(ctx context.Context, in StopTurnInput) error {
	if s == nil {
		return fmt.Errorf("service is required")
	}
	if err := validateStopTurnInput(in); err != nil {
		return err
	}

	execution, ok := s.stopActiveExecution(in.SessionID, in.TurnID, in.Reason)
	if !ok {
		return fmt.Errorf("active turn not found")
	}

	if execution == nil {
		return fmt.Errorf("active turn not found")
	}

	_ = ctx
	return nil
}

func (s *Service) startTurn(ctx context.Context, sessionID, agentID, message string, initiatorType InitiatorType, createIfMissing bool) (StartTurnResult, error) {
	now := s.now().UTC()
	sessionModel, err := s.ensureSession(ctx, sessionID, now, createIfMissing)
	if err != nil {
		return StartTurnResult{}, err
	}
	previousTurnID := sessionModel.LastTurnID

	turnID := NewTurnID()
	runID := NewRunID()
	eventID := NewEventID()
	externalTaskID := NewExternalTaskID()

	turnModel := Turn{
		TurnID:        turnID,
		SessionID:     sessionID,
		InitiatorType: initiatorType,
		InputSummary:  message,
		Status:        TurnRunning,
		PrimaryRunID:  runID,
		CreatedAt:     now,
	}
	if err := s.store.CreateTurn(ctx, turnModel); err != nil {
		return StartTurnResult{}, fmt.Errorf("create turn: %w", err)
	}

	runModel := Run{
		RunID:     runID,
		SessionID: sessionID,
		TurnID:    turnID,
		AgentID:   agentID,
		State:     RunQueued,
		StartedAt: now,
	}
	if err := s.store.CreateRun(ctx, runModel); err != nil {
		return StartTurnResult{}, fmt.Errorf("create run: %w", err)
	}

	sessionModel.LastTurnID = turnID
	sessionModel.UpdatedAt = now
	sessionModel.Status = SessionRunning
	if err := s.store.UpdateSession(ctx, sessionModel); err != nil {
		return StartTurnResult{}, fmt.Errorf("update session head: %w", err)
	}

	eventModel := RunEvent{
		EventID:   eventID,
		SessionID: sessionID,
		TurnID:    turnID,
		RunID:     runID,
		AgentID:   agentID,
		EventType: EventRunQueued,
		Sequence:  1,
		Payload: map[string]any{
			"message": message,
		},
		Source:    "host.session.service",
		CreatedAt: now,
	}
	eventModel, err = s.appendAndPublishEvent(ctx, eventModel)
	if err != nil {
		return StartTurnResult{}, fmt.Errorf("append queued event: %w", err)
	}
	startedEvent := RunEvent{
		EventID:   NewEventID(),
		SessionID: sessionID,
		TurnID:    turnID,
		RunID:     runID,
		AgentID:   agentID,
		EventType: EventRunStarted,
		Sequence:  2,
		Payload: map[string]any{
			"message": message,
		},
		Source:    "host.session.service",
		CreatedAt: now,
	}
	startedEvent, err = s.appendAndPublishEvent(ctx, startedEvent)
	if err != nil {
		return StartTurnResult{}, fmt.Errorf("append started event: %w", err)
	}

	if s.runner != nil {
		runCtx, execution := s.registerActiveExecution(ctx, sessionID, turnID, runID, agentID)
		runCtx = hitl.WithTicketContext(runCtx, hitl.TicketContext{
			SessionID: sessionID,
			TurnID:    turnID,
			RunID:     runID,
		})
		result, err := s.runner.Run(runCtx, RuntimeRequest{
			SessionID:   sessionID,
			TurnID:      turnID,
			RunID:       runID,
			AgentID:     agentID,
			Instruction: message,
			Context:     s.buildRuntimeContext(ctx, sessionID, previousTurnID, externalTaskID),
			Emit: func(runtimeEvent RuntimeEvent) error {
				if strings.TrimSpace(runtimeEvent.EventType) == "" {
					return nil
				}
				payload := mergeRuntimePayload(runtimeEvent.Payload, map[string]any{
					"externalTaskId": externalTaskID,
				})

				if err := s.applyRuntimeStatus(ctx, sessionModel, turnID, runID, runtimeEvent); err != nil {
					return fmt.Errorf("apply runtime status: %w", err)
				}

				event := RunEvent{
					EventID:   NewEventID(),
					SessionID: sessionID,
					TurnID:    turnID,
					RunID:     runID,
					AgentID:   agentID,
					EventType: runtimeEvent.EventType,
					Payload:   payload,
					Source:    "host.session.runtime",
					CreatedAt: s.now().UTC(),
				}
				if _, err := s.appendAndPublishEvent(ctx, event); err != nil {
					return fmt.Errorf("append runtime event: %w", err)
				}
				return nil
			},
		})
		execution = s.finishActiveExecution(sessionID, turnID, runID)
		if execution != nil && execution.stopped && errors.Is(err, context.Canceled) {
			canceledAt := s.now().UTC()
			reason := strings.TrimSpace(execution.reason)
			if reason == "" {
				reason = "stopped by operator"
			}
			if lifecycleErr := s.persistLifecycleState(ctx, sessionModel, turnID, runID, RunCanceled, TurnWaiting, "", reason, canceledAt); lifecycleErr != nil {
				return StartTurnResult{}, fmt.Errorf("persist canceled lifecycle: %w", lifecycleErr)
			}
			canceledEvent := RunEvent{
				EventID:   NewEventID(),
				SessionID: sessionID,
				TurnID:    turnID,
				RunID:     runID,
				AgentID:   agentID,
				EventType: EventRunCanceled,
				Payload: map[string]any{
					"reason": reason,
				},
				Source:    "host.session.service",
				CreatedAt: canceledAt,
			}
			if _, appendErr := s.appendAndPublishEvent(ctx, canceledEvent); appendErr != nil {
				return StartTurnResult{}, fmt.Errorf("append canceled event: %w", appendErr)
			}
			return StartTurnResult{
				SessionID: sessionID,
				TurnID:    turnID,
				RunID:     runID,
			}, nil
		}
		if err != nil {
			failedAt := s.now().UTC()
			if lifecycleErr := s.persistLifecycleState(ctx, sessionModel, turnID, runID, RunFailed, TurnFailed, "", err.Error(), failedAt); lifecycleErr != nil {
				return StartTurnResult{}, fmt.Errorf("persist failed lifecycle: %w", lifecycleErr)
			}
			failedEvent := RunEvent{
				EventID:   NewEventID(),
				SessionID: sessionID,
				TurnID:    turnID,
				RunID:     runID,
				AgentID:   agentID,
				EventType: EventRunFailed,
				Payload: map[string]any{
					"error": err.Error(),
				},
				Source:    "host.session.service",
				CreatedAt: now,
			}
			if _, appendErr := s.appendAndPublishEvent(ctx, failedEvent); appendErr != nil {
				return StartTurnResult{}, fmt.Errorf("append failed event: %w", appendErr)
			}
			return StartTurnResult{}, fmt.Errorf("run runtime request: %w", err)
		}
		if result.ExternalTask != nil {
			// External task snapshots are written before the finished event so the
			// client can immediately query run-linked delegation state.
			if err := s.persistExternalTask(ctx, sessionID, turnID, runID, result.ExternalTask); err != nil {
				return StartTurnResult{}, fmt.Errorf("persist external task: %w", err)
			}
		}
		if isWaitingRunState(result.WaitState) {
			// Waiting states are non-terminal host outcomes. The runtime is expected
			// to have emitted the matching run.status event before returning.
			return StartTurnResult{
				SessionID: sessionID,
				TurnID:    turnID,
				RunID:     runID,
			}, nil
		}
		finishedAt := s.now().UTC()
		if err := s.persistLifecycleState(ctx, sessionModel, turnID, runID, RunCompleted, TurnCompleted, result.Summary, "", finishedAt); err != nil {
			return StartTurnResult{}, fmt.Errorf("persist finished lifecycle: %w", err)
		}

		finishedEvent := RunEvent{
			EventID:   NewEventID(),
			SessionID: sessionID,
			TurnID:    turnID,
			RunID:     runID,
			AgentID:   agentID,
			EventType: EventRunFinished,
			Payload: map[string]any{
				"summary": result.Summary,
			},
			Source:    "host.session.service",
			CreatedAt: now,
		}
		if _, err := s.appendAndPublishEvent(ctx, finishedEvent); err != nil {
			return StartTurnResult{}, fmt.Errorf("append finished event: %w", err)
		}
	}

	return StartTurnResult{
		SessionID: sessionID,
		TurnID:    turnID,
		RunID:     runID,
	}, nil
}

func (s *Service) registerActiveExecution(
	ctx context.Context,
	sessionID string,
	turnID string,
	runID string,
	agentID string,
) (context.Context, *activeExecution) {
	runCtx, cancel := context.WithCancel(ctx)
	execution := &activeExecution{
		sessionID: sessionID,
		turnID:    turnID,
		runID:     runID,
		agentID:   agentID,
		cancel:    cancel,
	}

	s.activeMu.Lock()
	s.activeExecutions[activeExecutionKey(sessionID, turnID, runID)] = execution
	s.activeMu.Unlock()

	return runCtx, execution
}

func (s *Service) stopActiveExecution(sessionID, turnID, reason string) (*activeExecution, bool) {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()

	for _, execution := range s.activeExecutions {
		if execution.sessionID != sessionID || execution.turnID != turnID {
			continue
		}
		execution.stopped = true
		execution.reason = strings.TrimSpace(reason)
		execution.cancel()
		return execution, true
	}

	return nil, false
}

func (s *Service) finishActiveExecution(sessionID, turnID, runID string) *activeExecution {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()

	key := activeExecutionKey(sessionID, turnID, runID)
	execution := s.activeExecutions[key]
	if execution == nil {
		return nil
	}
	delete(s.activeExecutions, key)
	return execution
}

func activeExecutionKey(sessionID, turnID, runID string) string {
	return sessionID + ":" + turnID + ":" + runID
}

func (s *Service) persistLifecycleState(
	ctx context.Context,
	sessionModel Session,
	turnID string,
	runID string,
	runState RunState,
	turnStatus TurnStatus,
	summary string,
	failureCode string,
	finishedAt time.Time,
) error {
	if s == nil || s.store == nil {
		return nil
	}

	// Keep the entity tables aligned with the emitted lifecycle events so
	// REST recovery and websocket snapshots do not disagree after reload.
	if err := s.store.UpdateRunLifecycle(ctx, runID, runState, summary, failureCode, &finishedAt); err != nil {
		return fmt.Errorf("update run lifecycle: %w", err)
	}
	if err := s.store.UpdateTurnStatus(ctx, turnID, turnStatus, &finishedAt); err != nil {
		return fmt.Errorf("update turn status: %w", err)
	}

	sessionModel.Status = SessionWaiting
	sessionModel.UpdatedAt = finishedAt
	if err := s.store.UpdateSession(ctx, sessionModel); err != nil {
		return fmt.Errorf("update session status: %w", err)
	}
	return nil
}

func (s *Service) buildRuntimeContext(ctx context.Context, sessionID, previousTurnID, externalTaskID string) map[string]any {
	ctxMap := map[string]any{}
	if strings.TrimSpace(externalTaskID) != "" {
		ctxMap["external_task_id"] = externalTaskID
	}
	if s == nil || s.store == nil {
		return ctxMap
	}
	if strings.TrimSpace(previousTurnID) == "" {
		return ctxMap
	}

	tasks, err := s.store.ListExternalTasksBySession(ctx, sessionID)
	if err != nil {
		return ctxMap
	}

	resumeTasks := make([]ExternalTask, 0, len(tasks))
	for _, task := range tasks {
		if task.TurnID != previousTurnID {
			continue
		}
		if strings.TrimSpace(task.RemoteSessionID) == "" {
			continue
		}
		resumeTasks = append(resumeTasks, task)
	}
	if len(resumeTasks) == 0 {
		return ctxMap
	}

	ctxMap["resume_turn_id"] = previousTurnID
	ctxMap["resume_external_tasks"] = resumeTasks
	return ctxMap
}

func mergeRuntimePayload(payload map[string]any, extra map[string]any) map[string]any {
	if payload == nil && extra == nil {
		return map[string]any{}
	}
	out := map[string]any{}
	for key, value := range payload {
		out[key] = value
	}
	for key, value := range extra {
		if _, ok := out[key]; ok {
			continue
		}
		out[key] = value
	}
	return out
}

func (s *Service) appendAndPublishEvent(ctx context.Context, event RunEvent) (RunEvent, error) {
	if s == nil || s.store == nil {
		return event, nil
	}

	storedEvent, err := s.store.AppendEvent(ctx, event)
	if err != nil {
		return RunEvent{}, err
	}
	if s.pub != nil {
		if err := s.pub.PublishSessionEvent(ctx, storedEvent); err != nil {
			return RunEvent{}, err
		}
	}
	return storedEvent, nil
}

func (s *Service) applyRuntimeStatus(ctx context.Context, sessionModel Session, turnID, runID string, event RuntimeEvent) error {
	if s == nil || s.store == nil {
		return nil
	}
	if strings.TrimSpace(event.EventType) != EventRunStatus {
		return nil
	}

	runState, ok := runtimeRunState(event.Payload)
	if !ok {
		return nil
	}

	switch runState {
	case RunDispatching, RunExternal:
		if err := s.store.UpdateRunLifecycle(ctx, runID, runState, "", "", nil); err != nil {
			return fmt.Errorf("update run runtime state: %w", err)
		}
		return nil
	case RunInputRequired, RunAuthRequired:
		if err := s.store.UpdateRunLifecycle(ctx, runID, runState, "", "", nil); err != nil {
			return fmt.Errorf("update waiting run state: %w", err)
		}
		if err := s.store.UpdateTurnStatus(ctx, turnID, TurnWaiting, nil); err != nil {
			return fmt.Errorf("update waiting turn status: %w", err)
		}
		sessionModel.Status = SessionWaiting
		sessionModel.UpdatedAt = s.now().UTC()
		if err := s.store.UpdateSession(ctx, sessionModel); err != nil {
			return fmt.Errorf("update waiting session status: %w", err)
		}
		return nil
	default:
		return nil
	}
}

func runtimeRunState(payload map[string]any) (RunState, bool) {
	if len(payload) == 0 {
		return "", false
	}
	raw, _ := payload["state"].(string)
	state := RunState(strings.TrimSpace(raw))
	switch state {
	case RunPreparing, RunThinking, RunDispatching, RunExternal, RunInputRequired, RunAuthRequired, RunSummarizing:
		return state, true
	default:
		return "", false
	}
}

func isWaitingRunState(state RunState) bool {
	switch state {
	case RunInputRequired, RunAuthRequired:
		return true
	default:
		return false
	}
}

func (s *Service) persistExternalTask(ctx context.Context, sessionID, turnID, runID string, task *ExternalTask) error {
	if task == nil {
		return nil
	}

	taskModel := *task
	if strings.TrimSpace(taskModel.ExternalTaskID) == "" {
		taskModel.ExternalTaskID = NewExternalTaskID()
	}
	if strings.TrimSpace(taskModel.SessionID) == "" {
		taskModel.SessionID = sessionID
	}
	if strings.TrimSpace(taskModel.TurnID) == "" {
		taskModel.TurnID = turnID
	}
	if strings.TrimSpace(taskModel.RunID) == "" {
		taskModel.RunID = runID
	}
	if taskModel.Metadata == nil {
		taskModel.Metadata = map[string]any{}
	}

	return s.store.CreateExternalTask(ctx, taskModel)
}

func (s *Service) ensureSession(ctx context.Context, sessionID string, now time.Time, createIfMissing bool) (Session, error) {
	sessionModel, err := s.store.GetSession(ctx, sessionID)
	if err == nil {
		return sessionModel, nil
	}
	if !errors.Is(err, ErrSessionNotFound) {
		return Session{}, err
	}
	if !createIfMissing {
		return Session{}, ErrSessionNotFound
	}

	sessionModel = Session{
		SessionID: sessionID,
		Title:     "Session " + sessionID,
		Status:    SessionOpen,
		CreatedAt: now,
		UpdatedAt: now,
		Metadata:  map[string]any{},
	}
	if err := s.store.CreateSession(ctx, sessionModel); err != nil {
		return Session{}, fmt.Errorf("create missing session: %w", err)
	}
	return sessionModel, nil
}

func validateStartTurnInput(in StartTurnInput) error {
	return validateTurnRequest(in.SessionID, in.AgentID, in.UserMessage, in.InitiatorType)
}

func validateContinueTurnInput(in ContinueTurnInput) error {
	return validateTurnRequest(in.SessionID, in.AgentID, in.Message, in.InitiatorType)
}

func validateStopTurnInput(in StopTurnInput) error {
	if strings.TrimSpace(in.SessionID) == "" {
		return fmt.Errorf("session id is required")
	}
	if strings.TrimSpace(in.TurnID) == "" {
		return fmt.Errorf("turn id is required")
	}
	return nil
}

func validateTurnRequest(sessionID, agentID, message string, initiatorType InitiatorType) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session id is required")
	}
	if strings.TrimSpace(agentID) == "" {
		return fmt.Errorf("agent id is required")
	}
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("user message is required")
	}
	if strings.TrimSpace(string(initiatorType)) == "" {
		return fmt.Errorf("initiator type is required")
	}
	return nil
}
