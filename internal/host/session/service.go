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
}

type RuntimeResult struct {
	Summary string
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

	now := s.now().UTC()
	sessionModel, err := s.ensureSession(ctx, in.SessionID, now)
	if err != nil {
		return StartTurnResult{}, err
	}

	turnID := NewTurnID()
	runID := NewRunID()
	eventID := NewEventID()

	turnModel := Turn{
		TurnID:        turnID,
		SessionID:     in.SessionID,
		InitiatorType: in.InitiatorType,
		InputSummary:  in.UserMessage,
		Status:        TurnRunning,
		PrimaryRunID:  runID,
		CreatedAt:     now,
	}
	if err := s.store.CreateTurn(ctx, turnModel); err != nil {
		return StartTurnResult{}, fmt.Errorf("create turn: %w", err)
	}

	runModel := Run{
		RunID:     runID,
		SessionID: in.SessionID,
		TurnID:    turnID,
		AgentID:   in.AgentID,
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
		SessionID: in.SessionID,
		TurnID:    turnID,
		RunID:     runID,
		AgentID:   in.AgentID,
		EventType: EventRunQueued,
		Sequence:  1,
		Payload: map[string]any{
			"message": in.UserMessage,
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

	if s.runner != nil {
		_, err := s.runner.Run(ctx, RuntimeRequest{
			SessionID:   in.SessionID,
			TurnID:      turnID,
			RunID:       runID,
			AgentID:     in.AgentID,
			Instruction: in.UserMessage,
		})
		if err != nil {
			return StartTurnResult{}, fmt.Errorf("run runtime request: %w", err)
		}
	}

	return StartTurnResult{
		SessionID: in.SessionID,
		TurnID:    turnID,
		RunID:     runID,
	}, nil
}

func (s *Service) ensureSession(ctx context.Context, sessionID string, now time.Time) (Session, error) {
	sessionModel, err := s.store.GetSession(ctx, sessionID)
	if err == nil {
		return sessionModel, nil
	}
	if !errors.Is(err, ErrSessionNotFound) {
		return Session{}, err
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
	if strings.TrimSpace(in.SessionID) == "" {
		return fmt.Errorf("session id is required")
	}
	if strings.TrimSpace(in.AgentID) == "" {
		return fmt.Errorf("agent id is required")
	}
	if strings.TrimSpace(in.UserMessage) == "" {
		return fmt.Errorf("user message is required")
	}
	if strings.TrimSpace(string(in.InitiatorType)) == "" {
		return fmt.Errorf("initiator type is required")
	}
	return nil
}
