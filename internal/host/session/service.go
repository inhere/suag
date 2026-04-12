package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
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

type StartTurnResult struct {
	SessionID string
	TurnID    string
	RunID     string
}

// Service owns the host-side lifecycle orchestration for session turns and runs.
type Service struct {
	store  Store
	pub    Publisher
	runner Runner
	now    func() time.Time
}

func NewService(store Store, pub Publisher, runner Runner, cfg ServiceConfig) *Service {
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	return &Service{
		store:  store,
		pub:    pub,
		runner: runner,
		now:    nowFn,
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
	if err := s.store.AppendEvent(ctx, eventModel); err != nil {
		return StartTurnResult{}, fmt.Errorf("append queued event: %w", err)
	}
	if s.pub != nil {
		if err := s.pub.PublishSessionEvent(ctx, eventModel); err != nil {
			return StartTurnResult{}, fmt.Errorf("publish queued event: %w", err)
		}
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
	if err := s.store.AppendEvent(ctx, startedEvent); err != nil {
		return StartTurnResult{}, fmt.Errorf("append started event: %w", err)
	}
	if s.pub != nil {
		if err := s.pub.PublishSessionEvent(ctx, startedEvent); err != nil {
			return StartTurnResult{}, fmt.Errorf("publish started event: %w", err)
		}
	}

	if s.runner != nil {
		nextSequence := int64(3)
		result, err := s.runner.Run(ctx, RuntimeRequest{
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

				event := RunEvent{
					EventID:   NewEventID(),
					SessionID: sessionID,
					TurnID:    turnID,
					RunID:     runID,
					AgentID:   agentID,
					EventType: runtimeEvent.EventType,
					Sequence:  nextSequence,
					Payload:   payload,
					Source:    "host.session.runtime",
					CreatedAt: s.now().UTC(),
				}
				nextSequence++
				if err := s.store.AppendEvent(ctx, event); err != nil {
					return fmt.Errorf("append runtime event: %w", err)
				}
				if s.pub != nil {
					if err := s.pub.PublishSessionEvent(ctx, event); err != nil {
						return fmt.Errorf("publish runtime event: %w", err)
					}
				}
				return nil
			},
		})
		if err != nil {
			failedEvent := RunEvent{
				EventID:   NewEventID(),
				SessionID: sessionID,
				TurnID:    turnID,
				RunID:     runID,
				AgentID:   agentID,
				EventType: EventRunFailed,
				Sequence:  nextSequence,
				Payload: map[string]any{
					"error": err.Error(),
				},
				Source:    "host.session.service",
				CreatedAt: now,
			}
			if appendErr := s.store.AppendEvent(ctx, failedEvent); appendErr != nil {
				return StartTurnResult{}, fmt.Errorf("append failed event: %w", appendErr)
			}
			if s.pub != nil {
				if publishErr := s.pub.PublishSessionEvent(ctx, failedEvent); publishErr != nil {
					return StartTurnResult{}, fmt.Errorf("publish failed event: %w", publishErr)
				}
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

		finishedEvent := RunEvent{
			EventID:   NewEventID(),
			SessionID: sessionID,
			TurnID:    turnID,
			RunID:     runID,
			AgentID:   agentID,
			EventType: EventRunFinished,
			Sequence:  nextSequence,
			Payload: map[string]any{
				"summary": result.Summary,
			},
			Source:    "host.session.service",
			CreatedAt: now,
		}
		if err := s.store.AppendEvent(ctx, finishedEvent); err != nil {
			return StartTurnResult{}, fmt.Errorf("append finished event: %w", err)
		}
		if s.pub != nil {
			if err := s.pub.PublishSessionEvent(ctx, finishedEvent); err != nil {
				return StartTurnResult{}, fmt.Errorf("publish finished event: %w", err)
			}
		}
	}

	return StartTurnResult{
		SessionID: sessionID,
		TurnID:    turnID,
		RunID:     runID,
	}, nil
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
