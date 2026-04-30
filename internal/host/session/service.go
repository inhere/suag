package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/inhere/axon/internal/ops/hitl"
)

var ErrSessionNotFound = errors.New("session not found")

type Runner interface {
	Run(ctx context.Context, in RuntimeRequest) (RuntimeResult, error)
}

type ContinueValidator interface {
	ValidateContinue(ctx context.Context, in RuntimeRequest) error
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
	WaitMetadata map[string]any
	Metadata     map[string]any
	ExternalTask *ExternalTask
}

type RuntimeEvent struct {
	EventType string
	Payload   map[string]any
}

type RuntimeContextBuildInput struct {
	SessionID       string
	AgentID         string
	Message         string
	IsContinue      bool
	SessionMetadata map[string]any
}

type RuntimeContextBuilder func(input RuntimeContextBuildInput) map[string]any

type SessionMetadataBuildInput struct {
	SessionID string
	AgentID   string
	Message   string
}

type SessionMetadataBuilder func(input SessionMetadataBuildInput) map[string]any

type ServiceConfig struct {
	Now                    func() time.Time
	SessionMetadataBuilder SessionMetadataBuilder
	RuntimeContextBuilder  RuntimeContextBuilder
	RecapHooks             []RecapHook
	PostTurnActionHooks    []PostTurnActionHook
	MemoryProjectionSync   MemoryProjectionSync
}

type RecapPhase string

const (
	RecapPhaseCompleted RecapPhase = "completed"
	RecapPhaseFailed    RecapPhase = "failed"
	RecapPhaseCanceled  RecapPhase = "canceled"
)

type RecapInput struct {
	Session Session
	TurnID  string
	RunID   string
	Phase   RecapPhase
	Summary string
	Failure string
}

type RecapHook interface {
	OnTurnRecap(ctx context.Context, input RecapInput) error
}

type PostTurnActionInput struct {
	Session        Session
	TurnID         string
	RunID          string
	Outcome        TurnOutcome
	Summary        string
	Failure        string
	RuntimeContext map[string]any
	LoopMetadata   map[string]any
	PlannerHint    *PlannerHint
	NextActionHint *NextActionHint
}

type PostTurnActionResult struct {
	PlannerHint    *PlannerHint
	NextActionHint *NextActionHint
}

type PostTurnActionHook interface {
	OnPostTurnAction(ctx context.Context, input PostTurnActionInput) (*PostTurnActionResult, error)
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

type UpdateMemoryCandidateInput struct {
	CandidateID string
	Status      MemoryCandidateStatus
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
	repo                   Repository
	pub                    Publisher
	runner                 Runner
	now                    func() time.Time
	sessionMetadataBuilder SessionMetadataBuilder
	runtimeContextBuilder  RuntimeContextBuilder
	recaps                 []RecapHook
	posts                  []PostTurnActionHook
	memory                 MemoryProjectionSync

	activeMu         sync.Mutex
	activeExecutions map[string]*activeExecution
}

func NewService(repo Repository, pub Publisher, runner Runner, cfg ServiceConfig) *Service {
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	return &Service{
		repo:                   repo,
		pub:                    pub,
		runner:                 runner,
		now:                    nowFn,
		sessionMetadataBuilder: cfg.SessionMetadataBuilder,
		runtimeContextBuilder:  cfg.RuntimeContextBuilder,
		recaps:                 append([]RecapHook(nil), cfg.RecapHooks...),
		posts:                  append([]PostTurnActionHook(nil), cfg.PostTurnActionHooks...),
		memory:                 cfg.MemoryProjectionSync,
		activeExecutions:       make(map[string]*activeExecution),
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
	if err := s.validateContinueTurnState(ctx, in.SessionID); err != nil {
		return StartTurnResult{}, err
	}
	if validator, ok := s.runner.(ContinueValidator); ok {
		runtimeContext, err := s.buildContinueRuntimeContext(ctx, in.SessionID, in.AgentID, in.Message)
		if err != nil {
			return StartTurnResult{}, err
		}
		if err := validator.ValidateContinue(ctx, RuntimeRequest{
			SessionID:   in.SessionID,
			AgentID:     in.AgentID,
			Instruction: in.Message,
			Context:     runtimeContext,
		}); err != nil {
			return StartTurnResult{}, err
		}
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

func (s *Service) UpdateMemoryCandidate(ctx context.Context, in UpdateMemoryCandidateInput) (MemoryCandidate, error) {
	if s == nil || s.repo == nil {
		return MemoryCandidate{}, fmt.Errorf("service is required")
	}
	if err := validateUpdateMemoryCandidateInput(in); err != nil {
		return MemoryCandidate{}, err
	}

	candidate, err := s.repo.GetMemoryCandidate(ctx, in.CandidateID)
	if err != nil {
		return MemoryCandidate{}, err
	}
	if candidate.Status == in.Status {
		return candidate, nil
	}

	if err := s.repo.UpdateMemoryCandidateStatus(ctx, in.CandidateID, in.Status); err != nil {
		return MemoryCandidate{}, err
	}
	candidate.Status = in.Status
	if s.memory != nil {
		if err := s.memory.RefreshSessionProjection(ctx, candidate.SessionID); err != nil {
			// Memory file refresh is best-effort. Candidate status should not roll
			// back when the markdown view cannot be updated immediately.
			slog.Warn("refresh memory files after candidate update failed",
				"candidate", candidate.CandidateID,
				"session", candidate.SessionID,
				"status", candidate.Status,
				"err", err,
			)
		}
	}

	return candidate, nil
}

func (s *Service) startTurn(ctx context.Context, sessionID, agentID, message string, initiatorType InitiatorType, createIfMissing bool) (StartTurnResult, error) {
	now := s.now().UTC()
	sessionModel, err := s.ensureSession(ctx, sessionID, now, createIfMissing)
	if err != nil {
		return StartTurnResult{}, err
	}
	if createIfMissing {
		if metadata := s.buildInitialSessionMetadata(sessionID, agentID, message); len(metadata) > 0 {
			if sessionModel.Metadata == nil {
				sessionModel.Metadata = map[string]any{}
			}
			for key, value := range metadata {
				sessionModel.Metadata[key] = value
			}
		}
	}
	previousTurnID := sessionModel.LastTurnID
	resumeFromRunID := ""
	if !createIfMissing && strings.TrimSpace(previousTurnID) != "" {
		resumeFromRunID, err = s.latestRunIDForTurn(ctx, sessionID, previousTurnID)
		if err != nil {
			return StartTurnResult{}, err
		}
	}

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
	if err := s.repo.CreateTurn(ctx, turnModel); err != nil {
		return StartTurnResult{}, fmt.Errorf("create turn: %w", err)
	}

	runModel := Run{
		RunID:           runID,
		SessionID:       sessionID,
		TurnID:          turnID,
		AgentID:         agentID,
		ResumeFromRunID: resumeFromRunID,
		State:           RunQueued,
		StartedAt:       now,
		Metadata:        s.initialRunMetadata(createIfMissing),
	}
	if err := s.repo.CreateRun(ctx, runModel); err != nil {
		return StartTurnResult{}, fmt.Errorf("create run: %w", err)
	}

	sessionModel.LastTurnID = turnID
	sessionModel.UpdatedAt = now
	sessionModel.Status = SessionRunning
	if err := s.repo.UpdateSession(ctx, sessionModel); err != nil {
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
		emittedTerminalEventTypes := make(map[string]bool, 3)
		emittedTerminalPayloads := make(map[string]map[string]any, 3)
		runCtx, execution := s.registerActiveExecution(ctx, sessionID, turnID, runID, agentID)
		runCtx = hitl.WithTicketContext(runCtx, hitl.TicketContext{
			SessionID: sessionID,
			TurnID:    turnID,
			RunID:     runID,
		})
		runtimeContext := s.buildRuntimeContext(ctx, sessionID, agentID, message, previousTurnID, resumeFromRunID, externalTaskID, !createIfMissing)
		result, err := s.runner.Run(runCtx, RuntimeRequest{
			SessionID:   sessionID,
			TurnID:      turnID,
			RunID:       runID,
			AgentID:     agentID,
			Instruction: message,
			Context:     runtimeContext,
			Emit: func(runtimeEvent RuntimeEvent) error {
				if strings.TrimSpace(runtimeEvent.EventType) == "" {
					return nil
				}
				if isTerminalRuntimeEvent(runtimeEvent.EventType) {
					emittedTerminalEventTypes[runtimeEvent.EventType] = true
					emittedTerminalPayloads[runtimeEvent.EventType] = mergeRuntimePayload(runtimeEvent.Payload, nil)
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
		if err == nil && len(result.Metadata) > 0 {
			if updateErr := s.updateRunMetadata(ctx, runID, result.Metadata); updateErr != nil {
				return StartTurnResult{}, fmt.Errorf("update runtime metadata: %w", updateErr)
			}
		}
		if execution != nil && execution.stopped && errors.Is(err, context.Canceled) {
			canceledAt := s.now().UTC()
			reason := strings.TrimSpace(execution.reason)
			if reason == "" {
				reason = "stopped by operator"
			}
			if lifecycleErr := s.persistLifecycleState(ctx, sessionModel, turnID, runID, RunCanceled, TurnCanceled, "", reason, canceledAt); lifecycleErr != nil {
				return StartTurnResult{}, fmt.Errorf("persist canceled lifecycle: %w", lifecycleErr)
			}
			if !emittedTerminalEventTypes[EventRunCanceled] {
				canceledEvent := RunEvent{
					EventID:   NewEventID(),
					SessionID: sessionID,
					TurnID:    turnID,
					RunID:     runID,
					AgentID:   agentID,
					EventType: EventRunCanceled,
					Payload: withExternalTaskID(map[string]any{
						"reason": reason,
					}, externalTaskID),
					Source:    "host.session.service",
					CreatedAt: canceledAt,
				}
				if _, appendErr := s.appendAndPublishEvent(ctx, canceledEvent); appendErr != nil {
					return StartTurnResult{}, fmt.Errorf("append canceled event: %w", appendErr)
				}
			}
			s.runRecapHooks(ctx, RecapInput{
				Session: sessionModel,
				TurnID:  turnID,
				RunID:   runID,
				Phase:   RecapPhaseCanceled,
				Failure: reason,
			})
			s.runPostTurnActionHooks(ctx, PostTurnActionInput{
				Session:        sessionModel,
				TurnID:         turnID,
				RunID:          runID,
				Outcome:        TurnOutcomeCanceled,
				Failure:        reason,
				RuntimeContext: cloneAnyMap(runtimeContext),
				LoopMetadata:   s.loadRunMetadata(ctx, runID),
			})
			return StartTurnResult{
				SessionID: sessionID,
				TurnID:    turnID,
				RunID:     runID,
			}, nil
		}
		if err != nil {
			failedExternalTaskID := externalTaskID
			if result.ExternalTask != nil {
				if persistErr := s.persistExternalTask(ctx, sessionID, turnID, runID, result.ExternalTask); persistErr != nil {
					return StartTurnResult{}, fmt.Errorf("persist failed external task: %w", persistErr)
				}
				if strings.TrimSpace(result.ExternalTask.ExternalTaskID) != "" {
					failedExternalTaskID = strings.TrimSpace(result.ExternalTask.ExternalTaskID)
				}
			}
			failedAt := s.now().UTC()
			if lifecycleErr := s.persistLifecycleState(ctx, sessionModel, turnID, runID, RunFailed, TurnFailed, "", err.Error(), failedAt); lifecycleErr != nil {
				return StartTurnResult{}, fmt.Errorf("persist failed lifecycle: %w", lifecycleErr)
			}
			if !emittedTerminalEventTypes[EventRunFailed] {
				failedEvent := RunEvent{
					EventID:   NewEventID(),
					SessionID: sessionID,
					TurnID:    turnID,
					RunID:     runID,
					AgentID:   agentID,
					EventType: EventRunFailed,
					Payload: withExternalTaskID(map[string]any{
						"error": err.Error(),
					}, failedExternalTaskID),
					Source:    "host.session.service",
					CreatedAt: now,
				}
				if _, appendErr := s.appendAndPublishEvent(ctx, failedEvent); appendErr != nil {
					return StartTurnResult{}, fmt.Errorf("append failed event: %w", appendErr)
				}
			}
			s.runRecapHooks(ctx, RecapInput{
				Session: sessionModel,
				TurnID:  turnID,
				RunID:   runID,
				Phase:   RecapPhaseFailed,
				Failure: err.Error(),
			})
			s.runPostTurnActionHooks(ctx, PostTurnActionInput{
				Session:        sessionModel,
				TurnID:         turnID,
				RunID:          runID,
				Outcome:        TurnOutcomeFailed,
				Failure:        err.Error(),
				RuntimeContext: cloneAnyMap(runtimeContext),
				LoopMetadata:   s.loadRunMetadata(ctx, runID),
			})
			return StartTurnResult{
				SessionID: sessionID,
				TurnID:    turnID,
				RunID:     runID,
			}, fmt.Errorf("run runtime request: %w", err)
		}
		if result.ExternalTask != nil {
			// External task snapshots are written before the finished event so the
			// client can immediately query run-linked delegation state.
			if err := s.persistExternalTask(ctx, sessionID, turnID, runID, result.ExternalTask); err != nil {
				return StartTurnResult{}, fmt.Errorf("persist external task: %w", err)
			}
		}
		if emittedTerminalEventTypes[EventRunCanceled] {
			canceledAt := s.now().UTC()
			canceledPayload := emittedTerminalPayloads[EventRunCanceled]
			reason, _ := canceledPayload["reason"].(string)
			reason = strings.TrimSpace(reason)
			if reason == "" {
				reason = "canceled"
			}
			if err := s.persistLifecycleState(ctx, sessionModel, turnID, runID, RunCanceled, TurnCanceled, "", reason, canceledAt); err != nil {
				return StartTurnResult{}, fmt.Errorf("persist runtime canceled lifecycle: %w", err)
			}
			s.runRecapHooks(ctx, RecapInput{
				Session: sessionModel,
				TurnID:  turnID,
				RunID:   runID,
				Phase:   RecapPhaseCanceled,
				Failure: reason,
			})
			s.runPostTurnActionHooks(ctx, PostTurnActionInput{
				Session:        sessionModel,
				TurnID:         turnID,
				RunID:          runID,
				Outcome:        TurnOutcomeCanceled,
				Failure:        reason,
				RuntimeContext: cloneAnyMap(runtimeContext),
				LoopMetadata:   s.loadRunMetadata(ctx, runID),
			})
			return StartTurnResult{
				SessionID: sessionID,
				TurnID:    turnID,
				RunID:     runID,
			}, nil
		}
		if emittedTerminalEventTypes[EventRunFailed] {
			failedAt := s.now().UTC()
			failedPayload := emittedTerminalPayloads[EventRunFailed]
			failure, _ := failedPayload["error"].(string)
			failure = strings.TrimSpace(failure)
			if failure == "" {
				failure = "runtime failed"
			}
			if err := s.persistLifecycleState(ctx, sessionModel, turnID, runID, RunFailed, TurnFailed, "", failure, failedAt); err != nil {
				return StartTurnResult{}, fmt.Errorf("persist runtime failed lifecycle: %w", err)
			}
			s.runRecapHooks(ctx, RecapInput{
				Session: sessionModel,
				TurnID:  turnID,
				RunID:   runID,
				Phase:   RecapPhaseFailed,
				Failure: failure,
			})
			s.runPostTurnActionHooks(ctx, PostTurnActionInput{
				Session:        sessionModel,
				TurnID:         turnID,
				RunID:          runID,
				Outcome:        TurnOutcomeFailed,
				Failure:        failure,
				RuntimeContext: cloneAnyMap(runtimeContext),
				LoopMetadata:   s.loadRunMetadata(ctx, runID),
			})
			return StartTurnResult{
				SessionID: sessionID,
				TurnID:    turnID,
				RunID:     runID,
			}, nil
		}
		if emittedTerminalEventTypes[EventRunFinished] {
			finishedAt := s.now().UTC()
			finishedPayload := emittedTerminalPayloads[EventRunFinished]
			summary, _ := finishedPayload["summary"].(string)
			summary = strings.TrimSpace(summary)
			if summary == "" {
				summary = strings.TrimSpace(result.Summary)
			}
			if err := s.persistLifecycleState(ctx, sessionModel, turnID, runID, RunCompleted, TurnCompleted, summary, "", finishedAt); err != nil {
				return StartTurnResult{}, fmt.Errorf("persist runtime finished lifecycle: %w", err)
			}
			s.runRecapHooks(ctx, RecapInput{
				Session: sessionModel,
				TurnID:  turnID,
				RunID:   runID,
				Phase:   RecapPhaseCompleted,
				Summary: summary,
			})
			s.runPostTurnActionHooks(ctx, PostTurnActionInput{
				Session:        sessionModel,
				TurnID:         turnID,
				RunID:          runID,
				Outcome:        TurnOutcomeCompleted,
				Summary:        summary,
				RuntimeContext: cloneAnyMap(runtimeContext),
				LoopMetadata:   s.loadRunMetadata(ctx, runID),
			})
			return StartTurnResult{
				SessionID: sessionID,
				TurnID:    turnID,
				RunID:     runID,
			}, nil
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

		if !emittedTerminalEventTypes[EventRunFinished] {
			finishedExternalTaskID := externalTaskID
			if result.ExternalTask != nil && strings.TrimSpace(result.ExternalTask.ExternalTaskID) != "" {
				finishedExternalTaskID = strings.TrimSpace(result.ExternalTask.ExternalTaskID)
			}
			finishedEvent := RunEvent{
				EventID:   NewEventID(),
				SessionID: sessionID,
				TurnID:    turnID,
				RunID:     runID,
				AgentID:   agentID,
				EventType: EventRunFinished,
				Payload: withExternalTaskID(map[string]any{
					"summary": result.Summary,
				}, finishedExternalTaskID),
				Source:    "host.session.service",
				CreatedAt: now,
			}
			if _, err := s.appendAndPublishEvent(ctx, finishedEvent); err != nil {
				return StartTurnResult{}, fmt.Errorf("append finished event: %w", err)
			}
		}
		s.runRecapHooks(ctx, RecapInput{
			Session: sessionModel,
			TurnID:  turnID,
			RunID:   runID,
			Phase:   RecapPhaseCompleted,
			Summary: result.Summary,
		})
		s.runPostTurnActionHooks(ctx, PostTurnActionInput{
			Session:        sessionModel,
			TurnID:         turnID,
			RunID:          runID,
			Outcome:        TurnOutcomeCompleted,
			Summary:        result.Summary,
			RuntimeContext: cloneAnyMap(runtimeContext),
			LoopMetadata:   s.loadRunMetadata(ctx, runID),
		})
	}

	return StartTurnResult{
		SessionID: sessionID,
		TurnID:    turnID,
		RunID:     runID,
	}, nil
}
