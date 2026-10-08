package personal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/inhere/suag/internal/host/session"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/storage/sqlite"
)

var safeSessionID = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Config holds the existing session and run authorities used by a personal agent.
type Config struct {
	Repository         session.Repository
	RunRoot            string
	Provider           agentcore.Provider
	Model              string
	ProviderName       string
	Tools              agentcore.ToolRegistry
	PromptBuilder      agentcore.PromptBuilder
	RuleSource         func() (string, string, error)
	AppendSystemPrompt string
	GoferJob           bool
	Hooks              []agentcore.Hook
	TranscriptDir      string
	MaxSteps           int
}

// Event is one transport-neutral lifecycle or model-stream update.
type Event struct {
	Kind      string
	SessionID string
	TurnID    string
	RunID     string
	Text      string
	ToolName  string
	Status    agentcore.RunStatus
	State     session.RunState
}

type EmitFunc func(context.Context, Event) error

type RunRequest struct {
	SessionID       string
	CreateIfMissing bool
	AgentID         string
	Prompt          string
	SystemContext   string
	EmitToolResults bool
	Emit            EmitFunc
}

type RunResult struct {
	SessionID string
	TurnID    string
	RunID     string
	Status    agentcore.RunStatus
	State     session.RunState
	Output    string
	Usage     agentcore.Usage
}

// Runner owns one synchronous turn at a time for a persisted personal session.
type Runner struct{ cfg Config }

func New(cfg Config) (*Runner, error) {
	if cfg.Repository == nil || cfg.Provider == nil || strings.TrimSpace(cfg.RunRoot) == "" {
		return nil, fmt.Errorf("personal runner requires repository, provider and run root")
	}
	return &Runner{cfg: cfg}, nil
}

func (r *Runner) NewSession(ctx context.Context, id, title string) (session.Session, error) {
	if id == "" {
		id = session.NewSessionID()
	}
	if err := validateSessionID(id); err != nil {
		return session.Session{}, err
	}
	now := time.Now().UTC()
	model := session.Session{
		SessionID: id,
		Title:     strings.TrimSpace(title),
		Status:    session.SessionOpen,
		CreatedAt: now,
		UpdatedAt: now,
		Metadata:  map[string]any{},
	}
	if model.Title == "" {
		model.Title = "Session " + id
	}
	if r.cfg.RuleSource != nil {
		rules, hash, err := r.cfg.RuleSource()
		if err != nil {
			return session.Session{}, err
		}
		model.Metadata["personal_rules_content"] = rules
		model.Metadata["personal_rules_hash"] = hash
	}
	model.Metadata["personal_append_prompt"] = r.cfg.AppendSystemPrompt
	model.Metadata["model"] = r.cfg.Model
	model.Metadata["provider"] = r.cfg.ProviderName
	if err := r.cfg.Repository.CreateSession(ctx, model); err != nil {
		return session.Session{}, fmt.Errorf("create personal session: %w", err)
	}
	return model, nil
}

func (r *Runner) LoadSession(ctx context.Context, id string) (session.Session, error) {
	if err := validateSessionID(id); err != nil {
		return session.Session{}, err
	}
	model, err := r.cfg.Repository.GetSession(ctx, id)
	if err != nil {
		return session.Session{}, fmt.Errorf("load personal session: %w", err)
	}
	return model, nil
}

// Run creates a new run for a completed turn, or resumes the yielded run in place.
func (r *Runner) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	if r == nil {
		return RunResult{}, fmt.Errorf("personal runner is nil")
	}
	if req.SessionID == "" && strings.TrimSpace(req.Prompt) == "" {
		return RunResult{}, fmt.Errorf("prompt is required to create a session")
	}
	model, err := r.findSession(ctx, req)
	if err != nil {
		return RunResult{}, err
	}
	if strings.TrimSpace(req.Prompt) == "" && model.Status != session.SessionWaiting {
		return RunResult{}, fmt.Errorf("prompt is required for a new turn")
	}
	if model.Status == session.SessionArchived || model.Status == session.SessionRunning {
		return RunResult{}, fmt.Errorf("session %q is not ready for a new turn: %s", model.SessionID, model.Status)
	}
	transcript := r.openTranscript(model.SessionID)
	if transcript != nil {
		defer func() {
			if err := transcript.Close(); err != nil {
				slog.Warn("close personal transcript", "session_id", model.SessionID, "err", err)
			}
		}()
		if req.SystemContext != "" {
			if err := transcript.AppendUser(req.SystemContext, true); err != nil {
				slog.Warn("append personal transcript", "session_id", model.SessionID, "err", err)
			}
		}
		if req.Prompt != "" {
			if err := transcript.AppendUser(req.Prompt, strings.HasPrefix(req.Prompt, "[gofer web 回复] ")); err != nil {
				slog.Warn("append personal transcript", "session_id", model.SessionID, "err", err)
			}
		}
	}

	storagePath := filepath.Join(r.cfg.RunRoot, model.SessionID, "runs.db")
	storage, err := sqlite.NewStorage(storagePath)
	if err != nil {
		return RunResult{}, fmt.Errorf("open run storage: %w", err)
	}
	defer storage.Close()

	result := RunResult{SessionID: model.SessionID}
	resumeMode := agentcore.ResumeAuto
	if model.Status == session.SessionWaiting {
		run, err := r.waitingRun(ctx, model)
		if err != nil {
			return result, err
		}
		result.TurnID, result.RunID = run.TurnID, run.RunID
		resumeMode = agentcore.ResumeFromState
		if strings.TrimSpace(req.Prompt) != "" {
			resumeMode = agentcore.ResumeWithInput
		}
	} else {
		result.TurnID, result.RunID = session.NewTurnID(), session.NewRunID()
	}
	priorMessages, priorMessagesErr := storage.LoadMessages(ctx, result.RunID)
	if priorMessagesErr != nil {
		priorMessages = nil
	}
	history, err := r.completedHistory(ctx, storage, model.SessionID, result.RunID)
	if err != nil {
		return result, err
	}
	var rulesUpdate string
	if r.cfg.RuleSource != nil {
		rules, hash, err := r.cfg.RuleSource()
		if err != nil {
			return result, err
		}
		if model.Metadata == nil {
			model.Metadata = map[string]any{}
		}
		if previous, _ := model.Metadata["personal_rules_hash"].(string); previous != hash {
			model.Metadata["personal_rules_hash"] = hash
			model.Metadata["personal_rules_content"] = rules
			rulesUpdate = "项目规则已更新\n" + rules
		}
	}

	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		agentID = "suag"
	}
	if model.Status == session.SessionWaiting {
		if err := r.cfg.Repository.UpdateTurnStatus(ctx, result.TurnID, session.TurnRunning, nil); err != nil {
			return result, err
		}
		if err := r.cfg.Repository.UpdateRunLifecycle(ctx, result.RunID, session.RunThinking, "", "", nil); err != nil {
			return result, err
		}
	} else {
		if err := r.createTurnAndRun(ctx, result, agentID, req.Prompt); err != nil {
			return result, err
		}
		model.LastTurnID = result.TurnID
	}
	model.Status = session.SessionRunning
	// Keep session metadata aligned with the provider selected for this turn.
	if model.Metadata == nil {
		model.Metadata = map[string]any{}
	}
	model.Metadata["model"] = r.cfg.Model
	model.Metadata["provider"] = r.cfg.ProviderName
	model.UpdatedAt = time.Now().UTC()
	if err := r.cfg.Repository.UpdateSession(ctx, model); err != nil {
		return result, err
	}
	if err := r.appendEvent(ctx, result, agentID, "personal.run.started", nil); err != nil {
		return r.finish(ctx, model, result, agentID, nil, err, req.Emit)
	}
	if err := emit(ctx, req.Emit, Event{Kind: "run_started", SessionID: result.SessionID, TurnID: result.TurnID, RunID: result.RunID}); err != nil {
		return r.finish(ctx, model, result, agentID, nil, err, req.Emit)
	}

	stream := func(ctx context.Context, update agentcore.StreamEvent) error {
		return emit(ctx, req.Emit, Event{
			Kind:      string(update.Kind),
			SessionID: result.SessionID,
			TurnID:    result.TurnID,
			RunID:     result.RunID,
			Text:      update.Delta,
		})
	}
	runtimeHooks := append([]agentcore.Hook(nil), r.cfg.Hooks...)
	if req.Emit != nil && req.EmitToolResults {
		runtimeHooks = append(runtimeHooks, toolResultEmitHook{sink: req.Emit, sessionID: result.SessionID, turnID: result.TurnID, runID: result.RunID})
	}
	runtime := agentcore.New(agentcore.Config{
		Provider:       r.cfg.Provider,
		Storage:        storage,
		Tools:          r.cfg.Tools,
		PromptBuilder:  rulesPromptBuilder{base: r.cfg.PromptBuilder, metadata: model.Metadata, goferJob: r.cfg.GoferJob},
		ContextBuilder: historyContext{previous: history, update: joinContext(rulesUpdate, req.SystemContext)},
		Hooks:          runtimeHooks,
		MaxSteps:       r.cfg.MaxSteps,
	})
	coreResult, runErr := runtime.Run(ctx, agentcore.RunInput{
		RunID:       result.RunID,
		AgentID:     agentID,
		Instruction: req.Prompt,
		ResumeMode:  resumeMode,
		Stream:      stream,
	})
	if messages, err := storage.LoadMessages(context.WithoutCancel(ctx), result.RunID); err != nil {
		slog.Warn("read personal run transcript", "session_id", model.SessionID, "err", err)
	} else {
		for _, message := range messages[min(len(priorMessages), len(messages)):] {
			if message.ToolResult == nil {
				continue
			}
			toolName := message.ToolResult.Name
			if transcript != nil {
				if err := transcript.AppendTool(toolName, message.ToolResult.Content); err != nil {
					slog.Warn("append personal transcript", "session_id", model.SessionID, "err", err)
				}
			}
		}
	}
	if coreResult != nil && transcript != nil {
		if err := transcript.AppendAssistant(coreResult.Output); err != nil {
			slog.Warn("append personal transcript", "session_id", model.SessionID, "err", err)
		}
	}
	return r.finish(ctx, model, result, agentID, coreResult, runErr, req.Emit)
}

type toolResultEmitHook struct {
	agentcore.NopHook
	sink      EmitFunc
	sessionID string
	turnID    string
	runID     string
}

func (h toolResultEmitHook) AfterTool(ctx context.Context, event *agentcore.ToolEvent, _ *agentcore.Control) error {
	if event == nil || event.Result == nil {
		return nil
	}
	return emit(ctx, h.sink, Event{
		Kind: "tool_result", SessionID: h.sessionID, TurnID: h.turnID, RunID: h.runID,
		ToolName: event.Result.Name, Text: event.Result.Content,
	})
}

func (r *Runner) openTranscript(sessionID string) *Transcript {
	if r.cfg.TranscriptDir == "" {
		return nil
	}
	transcript, err := OpenTranscript(r.cfg.TranscriptDir, sessionID)
	if err != nil {
		slog.Warn("open personal transcript", "session_id", sessionID, "err", err)
		return nil
	}
	return transcript
}

func joinContext(first, second string) string {
	if first == "" {
		return second
	}
	if second == "" {
		return first
	}
	return first + "\n\n" + second
}

func (r *Runner) findSession(ctx context.Context, req RunRequest) (session.Session, error) {
	if req.SessionID == "" {
		return r.NewSession(ctx, "", req.Prompt)
	}
	model, err := r.LoadSession(ctx, req.SessionID)
	if err == nil {
		return model, nil
	}
	if req.CreateIfMissing && errors.Is(err, session.ErrSessionNotFound) {
		if strings.TrimSpace(req.Prompt) == "" {
			return session.Session{}, fmt.Errorf("prompt is required to create session %q", req.SessionID)
		}
		return r.NewSession(ctx, req.SessionID, req.Prompt)
	}
	return session.Session{}, err
}

func (r *Runner) waitingRun(ctx context.Context, model session.Session) (session.Run, error) {
	events, err := r.cfg.Repository.ListEventsBySession(ctx, model.SessionID, 0)
	if err != nil {
		return session.Run{}, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].TurnID != model.LastTurnID || events[i].RunID == "" {
			continue
		}
		run, err := r.cfg.Repository.GetRun(ctx, events[i].RunID)
		if err != nil {
			return session.Run{}, err
		}
		if run.State != session.RunInputRequired && run.State != session.RunAuthRequired {
			return session.Run{}, fmt.Errorf("waiting session %q has non-waiting run %q: %s", model.SessionID, run.RunID, run.State)
		}
		return run, nil
	}
	return session.Run{}, fmt.Errorf("waiting session %q has no run to resume", model.SessionID)
}

func (r *Runner) completedHistory(ctx context.Context, storage agentcore.Storage, sessionID, currentRunID string) ([]agentcore.Message, error) {
	events, err := r.cfg.Repository.ListEventsBySession(ctx, sessionID, 0)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var history []agentcore.Message
	for _, event := range events {
		if event.RunID == "" || event.RunID == currentRunID || seen[event.RunID] {
			continue
		}
		seen[event.RunID] = true
		run, err := r.cfg.Repository.GetRun(ctx, event.RunID)
		if err != nil {
			return nil, fmt.Errorf("load prior run %q: %w", event.RunID, err)
		}
		if run.State != session.RunCompleted {
			continue
		}
		messages, err := storage.LoadMessages(ctx, run.RunID)
		if err != nil {
			return nil, fmt.Errorf("load prior transcript %q: %w", run.RunID, err)
		}
		history = append(history, messages...)
	}
	return history, nil
}

func (r *Runner) createTurnAndRun(ctx context.Context, result RunResult, agentID, prompt string) error {
	now := time.Now().UTC()
	if err := r.cfg.Repository.CreateTurn(ctx, session.Turn{
		TurnID:        result.TurnID,
		SessionID:     result.SessionID,
		InitiatorType: session.InitiatorUser,
		InputSummary:  prompt,
		Status:        session.TurnRunning,
		PrimaryRunID:  result.RunID,
		CreatedAt:     now,
	}); err != nil {
		return fmt.Errorf("create turn: %w", err)
	}
	if err := r.cfg.Repository.CreateRun(ctx, session.Run{
		RunID:     result.RunID,
		SessionID: result.SessionID,
		TurnID:    result.TurnID,
		AgentID:   agentID,
		State:     session.RunPreparing,
		StartedAt: now,
	}); err != nil {
		return fmt.Errorf("create run: %w", err)
	}
	return nil
}

func (r *Runner) finish(ctx context.Context, model session.Session, result RunResult, agentID string, coreResult *agentcore.RunResult, runErr error, sink EmitFunc) (RunResult, error) {
	if coreResult != nil {
		result.Status, result.Output, result.Usage = coreResult.Status, coreResult.Output, coreResult.Usage
	}
	if coreResult == nil && runErr == nil {
		runErr = fmt.Errorf("runtime returned no result")
	}
	if runErr != nil {
		result.Status = agentcore.RunFailed
	}
	if ctx.Err() != nil {
		result.Status = agentcore.RunFailed
	}

	runStatus, turnStatus, sessionStatus := session.RunCompleted, session.TurnCompleted, session.SessionOpen
	failureCode := ""
	switch result.Status {
	case agentcore.RunCompleted:
	case agentcore.RunYielded:
		runStatus, turnStatus, sessionStatus = session.RunInputRequired, session.TurnWaiting, session.SessionWaiting
	case agentcore.RunFailed:
		if ctx.Err() != nil {
			runStatus, turnStatus = session.RunCanceled, session.TurnCanceled
			failureCode = "cancelled"
		} else {
			runStatus, turnStatus = session.RunFailed, session.TurnFailed
			failureCode = "run_failed"
		}
	default:
		runStatus, turnStatus = session.RunFailed, session.TurnFailed
		failureCode = "unexpected_status"
	}
	result.State = runStatus
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	now := time.Now().UTC()
	var endedAt *time.Time
	if result.Status != agentcore.RunYielded {
		endedAt = &now
	}
	var persistErr error
	if err := r.cfg.Repository.UpdateRunLifecycle(finishCtx, result.RunID, runStatus, result.Output, failureCode, endedAt); err != nil {
		persistErr = errors.Join(persistErr, err)
	}
	if err := r.cfg.Repository.UpdateTurnStatus(finishCtx, result.TurnID, turnStatus, endedAt); err != nil {
		persistErr = errors.Join(persistErr, err)
	}
	model.Status, model.UpdatedAt = sessionStatus, now
	if err := r.cfg.Repository.UpdateSession(finishCtx, model); err != nil {
		persistErr = errors.Join(persistErr, err)
	}
	if err := r.appendEvent(finishCtx, result, agentID, "personal.run."+strings.ToLower(string(runStatus)), map[string]any{"status": string(runStatus)}); err != nil {
		persistErr = errors.Join(persistErr, err)
	}
	if err := emit(finishCtx, sink, Event{Kind: "run_finished", SessionID: result.SessionID, TurnID: result.TurnID, RunID: result.RunID, Status: result.Status, State: result.State, Text: result.Output}); err != nil {
		persistErr = errors.Join(persistErr, err)
	}
	return result, errors.Join(runErr, persistErr)
}

func (r *Runner) appendEvent(ctx context.Context, result RunResult, agentID, eventType string, payload map[string]any) error {
	_, err := r.cfg.Repository.AppendEvent(ctx, session.RunEvent{
		EventID:   session.NewEventID(),
		SessionID: result.SessionID,
		TurnID:    result.TurnID,
		RunID:     result.RunID,
		AgentID:   agentID,
		EventType: eventType,
		Payload:   payload,
		Source:    "personal",
		CreatedAt: time.Now().UTC(),
	})
	return err
}

func validateSessionID(id string) error {
	if id == "." || id == ".." || !safeSessionID.MatchString(id) {
		return fmt.Errorf("invalid session ID %q", id)
	}
	return nil
}

func emit(ctx context.Context, sink EmitFunc, event Event) error {
	if sink == nil {
		return nil
	}
	return sink(ctx, event)
}

type historyContext struct {
	previous []agentcore.Message
	update   string
}

func (b historyContext) BuildMessages(ctx context.Context, state *agentcore.RunState) ([]agentcore.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]agentcore.Message, 0, len(b.previous)+len(state.Messages))
	out = append(out, b.previous...)
	if b.update != "" {
		out = append(out, agentcore.Message{Role: "system", Content: b.update})
	}
	out = append(out, state.Messages...)
	return out, nil
}

type rulesPromptBuilder struct {
	base     agentcore.PromptBuilder
	metadata map[string]any
	goferJob bool
}

func (b rulesPromptBuilder) BuildSystemPrompt(ctx context.Context, state *agentcore.RunState) (string, error) {
	var parts []string
	if b.base != nil {
		base, err := b.base.BuildSystemPrompt(ctx, state)
		if err != nil {
			return "", err
		}
		parts = append(parts, base)
	}
	for _, key := range []string{"personal_rules_content", "personal_append_prompt"} {
		if value, _ := b.metadata[key].(string); value != "" {
			parts = append(parts, value)
		}
	}
	if b.goferJob {
		parts = append(parts, "当前运行在 Gofer job 中；可用 gofer work report / gofer issue 汇报工作进度与结果。")
	}
	return strings.Join(parts, "\n\n"), nil
}
