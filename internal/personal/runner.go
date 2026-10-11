package personal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/inhere/suag/internal/personal/process"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/redact"
	"github.com/inhere/suag/pkg/agentcore/tools"
	"github.com/inhere/suag/pkg/agentcore/tools/native"
)

var safeSessionID = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Session is a persisted conversation (alias of the core store model).
type Session = store.Session

// ErrSessionNotFound is returned when a session id does not exist.
var ErrSessionNotFound = store.ErrSessionNotFound

// RunState is the caller-facing run status reported in events and results
// (stream-json "status"). The values are a wire contract; do not change them.
type RunState string

const (
	RunCompleted     RunState = "COMPLETED"
	RunInputRequired RunState = "INPUT_REQUIRED"
	RunFailed        RunState = "FAILED"
	RunCanceled      RunState = "CANCELED"
)

// Config holds the core store and the model/tool wiring used by a personal agent.
type Config struct {
	// Repository is the single core store: sessions, turns, entries and
	// agentcore run state (run_id == turn_id) all live in it.
	Repository         *store.Store
	Provider           agentcore.Provider
	Model              string
	ProviderName       string
	Tools              agentcore.ToolRegistry
	PromptBuilder      agentcore.PromptBuilder
	RuleSource         func() (string, string, error)
	AppendSystemPrompt string
	// AppendSystemPromptSet marks AppendSystemPrompt as explicitly supplied by
	// the caller, so an empty value clears the saved prompt instead of keeping it.
	AppendSystemPromptSet bool
	Hooks                 []agentcore.Hook
	TranscriptDir         string
	LockRoot              string
	ProcessRoot           string
	MaxSteps              int
	// ContextWindow is the effective model's context window in tokens (0 = 128000).
	ContextWindow int
	// CompactReserveTokens / CompactKeepTokens override the compaction reserve
	// (default 16000) and the recent tail kept verbatim (default 20000).
	CompactReserveTokens int
	CompactKeepTokens    int
	// CompactProvider/CompactModel summarize history when llm.compact_model is
	// set; nil means the turn's own provider is used.
	CompactProvider agentcore.Provider
	CompactModel    string
	// Plans is the update_plan sink handed to the tool at build time; New binds
	// it to this runner's store.
	Plans *PlanRecorder
	// Visibility is the deferred-tool layer already wrapped around Tools; New
	// binds it to this runner's store (loaded_tools meta) and event recorder,
	// and its tool list is added to the system prompt.
	Visibility *tools.Visibility
}

// Event is one transport-neutral lifecycle or model-stream update.
type Event struct {
	Kind      string
	SessionID string
	TurnID    string
	RunID     string
	Text      string
	ToolName  string
	// IsError is set on "tool_result" events whose tool call failed or was refused.
	IsError bool
	// Plan is set on "plan" events (the full replaced task list).
	Plan   *native.Plan
	Status agentcore.RunStatus
	State  RunState
}

type EmitFunc func(context.Context, Event) error

type RunRequest struct {
	SessionID       string
	CreateIfMissing bool
	AgentID         string
	Prompt          string
	SystemContext   string
	Injected        bool
	Emit            EmitFunc
}

type RunResult struct {
	SessionID string
	TurnID    string
	RunID     string
	Status    agentcore.RunStatus
	State     RunState
	Output    string
	// Usage is this invocation's usage (provider-normalized: input includes cache).
	Usage agentcore.Usage
	// SessionUsage is the session-cumulative usage persisted after this turn.
	SessionUsage agentcore.Usage
	// ModelCalls counts the successful model calls of this invocation,
	// including those of a failed or cancelled turn.
	ModelCalls int
	// LastInputTokens is the input tokens (cache included) of the most recent
	// model call, i.e. the context size of the last request; 0 without a call.
	LastInputTokens int
	// DeniedTools lists the distinct tools refused by the approval layer in
	// this turn (first-refusal order); empty when nothing was refused.
	DeniedTools []string
	// DenialLimitReached is true when the turn was ended because DenialLimit
	// refusals accumulated (ACP maps it to stopReason refusal).
	DenialLimitReached bool
}

// Runner owns one synchronous turn at a time for a persisted personal session.
type Runner struct {
	cfg       Config
	locksMu   sync.Mutex
	heldLocks map[string]*process.SessionLock
	rec       *recorder
}

func New(cfg Config) (*Runner, error) {
	if cfg.Repository == nil || cfg.Provider == nil {
		return nil, fmt.Errorf("personal runner requires repository and provider")
	}
	// Observation wraps the provider and tools once here, so every model call
	// (including later compaction calls) and tool call emits events.
	if cfg.Plans != nil {
		cfg.Plans.bind(cfg.Repository)
	}
	rec := &recorder{sink: cfg.Repository}
	cfg.Provider = rec.observeProvider(cfg.Provider, cfg.ProviderName, cfg.Model)
	cfg.Tools = rec.observeTools(cfg.Tools)
	if cfg.CompactProvider != nil {
		cfg.CompactProvider = rec.observeProvider(cfg.CompactProvider, cfg.ProviderName, cfg.CompactModel)
	}
	runner := &Runner{cfg: cfg, heldLocks: make(map[string]*process.SessionLock), rec: rec}
	runner.bindVisibility()
	return runner, nil
}

// HoldSessionLock reserves a session for the lifetime of an interactive chat.
func (r *Runner) HoldSessionLock(sessionID string) (func() error, error) {
	if r.cfg.LockRoot == "" {
		return func() error { return nil }, nil
	}
	r.locksMu.Lock()
	if _, exists := r.heldLocks[sessionID]; exists {
		r.locksMu.Unlock()
		return func() error { return nil }, nil
	}
	r.locksMu.Unlock()
	lock, err := process.AcquireSessionLock(r.cfg.LockRoot, sessionID, 0)
	if err != nil {
		return nil, err
	}
	r.locksMu.Lock()
	if existing := r.heldLocks[sessionID]; existing != nil {
		r.locksMu.Unlock()
		_ = lock.Close()
		return func() error { return nil }, nil
	}
	r.heldLocks[sessionID] = lock
	r.locksMu.Unlock()
	var once sync.Once
	return func() error {
		var closeErr error
		once.Do(func() {
			r.locksMu.Lock()
			delete(r.heldLocks, sessionID)
			r.locksMu.Unlock()
			closeErr = lock.Close()
		})
		return closeErr
	}, nil
}

func (r *Runner) ProcessRoot() string { return r.cfg.ProcessRoot }

// ProviderName and Model identify the provider config id and model this runner
// calls; chat shows them in its per-turn status line.
func (r *Runner) ProviderName() string { return r.cfg.ProviderName }
func (r *Runner) Model() string        { return r.cfg.Model }

func (r *Runner) NewSession(ctx context.Context, id, title string) (Session, error) {
	if id == "" {
		id = NewSessionID()
	}
	if err := validateSessionID(id); err != nil {
		return Session{}, err
	}
	now := time.Now().UTC()
	model := Session{
		ID:        id,
		Title:     strings.TrimSpace(title),
		Status:    store.SessionOpen,
		CreatedAt: now,
		UpdatedAt: now,
		Meta:      map[string]any{},
	}
	if model.Title == "" {
		model.Title = "Session " + id
	}
	if r.cfg.RuleSource != nil {
		rules, hash, err := r.cfg.RuleSource()
		if err != nil {
			return Session{}, err
		}
		model.Meta["personal_rules_content"] = rules
		model.Meta["personal_rules_hash"] = hash
	}
	model.Meta["personal_append_prompt"] = r.cfg.AppendSystemPrompt
	model.Meta["model"] = r.cfg.Model
	model.Meta["provider"] = r.cfg.ProviderName
	if err := r.cfg.Repository.CreateSession(ctx, model); err != nil {
		return Session{}, fmt.Errorf("create personal session: %w", err)
	}
	return model, nil
}

// SetSessionTitle updates the persisted display title after an interactive chat's first prompt.
func (r *Runner) SetSessionTitle(ctx context.Context, sessionID, title string) error {
	model, err := r.LoadSession(ctx, sessionID)
	if err != nil {
		return err
	}
	model.Title = strings.TrimSpace(title)
	model.UpdatedAt = time.Now().UTC()
	if err := r.cfg.Repository.UpdateSession(ctx, model); err != nil {
		return fmt.Errorf("update personal session title: %w", err)
	}
	return nil
}

func (r *Runner) LoadSession(ctx context.Context, id string) (Session, error) {
	if err := validateSessionID(id); err != nil {
		return Session{}, err
	}
	model, err := r.cfg.Repository.GetSession(ctx, id)
	if err != nil {
		return Session{}, fmt.Errorf("load personal session: %w", err)
	}
	return model, nil
}

// Run executes one new turn (run_id == turn_id) of the session. A turn that
// yielded for input is finished as INPUT_REQUIRED and is not resumed in place:
// the caller's next prompt is simply a new turn that sees it as history.
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
	if strings.TrimSpace(req.Prompt) == "" {
		return RunResult{}, fmt.Errorf("prompt is required for a new turn")
	}
	if model.Status == store.SessionArchived || model.Status == store.SessionRunning {
		return r.withSessionUsage(ctx, model.ID, RunResult{SessionID: model.ID}), fmt.Errorf("session %q is not ready for a new turn: %s", model.ID, model.Status)
	}
	r.locksMu.Lock()
	_, heldByChat := r.heldLocks[model.ID]
	r.locksMu.Unlock()
	if r.cfg.LockRoot != "" && !heldByChat {
		lock, err := process.AcquireSessionLockContext(ctx, r.cfg.LockRoot, model.ID, 30*time.Second)
		if err != nil {
			return r.withSessionUsage(ctx, model.ID, RunResult{SessionID: model.ID}), err
		}
		defer lock.Close()
	}
	transcript := r.openTranscript(model.ID)
	if transcript != nil {
		defer func() {
			if err := transcript.Close(); err != nil {
				slog.Warn("close personal transcript", "session_id", model.ID, "err", err)
			}
		}()
		appendTurnInput(transcript, model.ID, req)
	}

	// run_id == turn_id: the store's agentcore adapter keys run state and
	// messages by turn id, and the turn row must exist before the runtime saves.
	turnID := NewTurnID()
	turnStart := time.Now()
	ctx = withObserveScope(ctx, model.ID, turnID)
	// Refused tool calls are counted per turn in ctx (see denial.go).
	ctx, denials := withTurnDenials(ctx)
	result := RunResult{SessionID: model.ID, TurnID: turnID, RunID: turnID}
	history, err := r.loadHistory(ctx, model.ID)
	if err != nil {
		return r.withSessionUsage(ctx, model.ID, result), err
	}

	metaPatch, notice, err := r.promptMetaPatch(model)
	if err != nil {
		return r.withSessionUsage(ctx, model.ID, result), err
	}

	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		agentID = "suag"
	}
	if err := r.cfg.Repository.CreateTurn(ctx, store.Turn{ID: turnID, SessionID: model.ID, Status: store.TurnRunning}); err != nil {
		return r.withSessionUsage(ctx, model.ID, result), fmt.Errorf("create turn: %w", err)
	}
	// Collect this turn's per-call usage (the session total comes from model events).
	usage := &usageCollector{}
	// Keep session metadata aligned with the provider selected for this turn.
	metaPatch["model"] = r.cfg.Model
	metaPatch["provider"] = r.cfg.ProviderName
	if err := r.cfg.Repository.PatchSessionMeta(ctx, model.ID, metaPatch); err != nil {
		return r.finish(ctx, model, result, turnStart, usage, nil, err, req.Emit)
	}
	// Re-read so the prompt builder and the status write see the patched,
	// current metadata (including usage written by another process).
	model, err = r.markRunning(ctx, model.ID, turnID)
	if err != nil {
		return r.finish(ctx, model, result, turnStart, usage, nil, err, req.Emit)
	}
	if err := emit(ctx, req.Emit, Event{Kind: "run_started", SessionID: result.SessionID, TurnID: result.TurnID, RunID: result.RunID}); err != nil {
		return r.finish(ctx, model, result, turnStart, usage, nil, err, req.Emit)
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
	// update_plan reaches the caller through this turn's Emit callback.
	ctx = withPlanEmitter(ctx, func(ctx context.Context, plan native.Plan) error {
		return emit(ctx, req.Emit, Event{Kind: "plan", SessionID: result.SessionID, TurnID: result.TurnID, RunID: result.RunID, Plan: &plan})
	})
	runtimeHooks := r.turnHooks(usage, denials, req.Emit, result)
	storage := r.cfg.Repository.AgentStorage()
	promptBuilder := rulesPromptBuilder{base: r.cfg.PromptBuilder, metadata: model.Meta, deferredTools: r.deferredToolsPrompt()}
	runtime := agentcore.New(agentcore.Config{
		Provider:       r.cfg.Provider,
		Storage:        storage,
		Tools:          r.cfg.Tools,
		PromptBuilder:  promptBuilder,
		ContextBuilder: r.newContextBuilder(model.ID, turnID, history, promptBuilder, usage, joinContext(notice, req.SystemContext)),
		Hooks:          runtimeHooks,
		MaxSteps:       r.cfg.MaxSteps,
		MaxStepsWrapUp: maxStepsWrapUp(r.cfg.MaxSteps),
		// Refused calls become tool results; aborted approvals end the turn.
		ToolExecutionPolicy: abortingToolPolicy{},
	})
	coreResult, runErr := runtime.Run(ctx, agentcore.RunInput{
		RunID:       result.RunID,
		AgentID:     agentID,
		Instruction: req.Prompt,
		ResumeMode:  agentcore.ResumeAuto,
		Stream:      stream,
	})
	if transcript != nil {
		appendTurnOutput(context.WithoutCancel(ctx), transcript, storage, model.ID, result.RunID, coreResult)
	}
	return r.finish(ctx, model, result, turnStart, usage, coreResult, runErr, req.Emit)
}

// appendTurnInput writes the turn's user lines to the session transcript.
func appendTurnInput(transcript *Transcript, sessionID string, req RunRequest) {
	if req.SystemContext != "" {
		if err := transcript.AppendUser(req.SystemContext, true); err != nil {
			slog.Warn("append personal transcript", "session_id", sessionID, "err", err)
		}
	}
	if req.Prompt != "" {
		// Only the caller knows whether a person typed this prompt.
		if err := transcript.AppendUser(req.Prompt, req.Injected); err != nil {
			slog.Warn("append personal transcript", "session_id", sessionID, "err", err)
		}
	}
}

// promptMetaPatch returns the session metadata keys this turn changes and the
// short notice for the model about them. The patch is applied with
// PatchSessionMeta so concurrent writers cannot be rolled back.
func (r *Runner) promptMetaPatch(model Session) (map[string]any, string, error) {
	metaPatch := map[string]any{}
	var rulesUpdate, appendUpdate string
	if r.cfg.RuleSource != nil {
		rules, hash, err := r.cfg.RuleSource()
		if err != nil {
			return nil, "", err
		}
		if previous, _ := model.Meta["personal_rules_hash"].(string); previous != hash {
			metaPatch["personal_rules_hash"] = hash
			metaPatch["personal_rules_content"] = rules
			// The system prompt is built from the metadata updated above, so it
			// already carries the new rules this turn. Only send a short notice
			// instead of repeating the full text (up to 128KB, resent per step).
			rulesUpdate = "项目规则已更新，以 system prompt 中的版本为准"
		}
	}

	// Resume with a caller-supplied append prompt (--append-system-prompt or
	// SUAG_APPEND_SYSTEM_PROMPT): when it differs from the persisted one, replace
	// it and send a short notice (same mechanism as rules; the full text lives
	// only in the system prompt). An unset value keeps the persisted one; an
	// explicitly supplied empty value clears it.
	if appended := r.cfg.AppendSystemPrompt; appended != "" || r.cfg.AppendSystemPromptSet {
		if previous, _ := model.Meta["personal_append_prompt"].(string); previous != appended {
			// An empty string (not nil) so PatchSessionMeta stores it instead of deleting the key.
			metaPatch["personal_append_prompt"] = appended
			appendUpdate = "附加提示已更新，以 system prompt 中的版本为准"
			if appended == "" {
				appendUpdate = "附加提示已清除，不再追加"
			}
		}
	}
	return metaPatch, joinContext(rulesUpdate, appendUpdate), nil
}

// turnHooks assembles the runtime hooks of one turn. The collector goes first
// so a later hook short-circuiting the chain cannot hide a model call from the
// usage total. The denial limit hook ends the turn before the next model call
// once DenialLimit refusals accumulated.
func (r *Runner) turnHooks(usage *usageCollector, denials *turnDenials, sink EmitFunc, result RunResult) []agentcore.Hook {
	hooks := append([]agentcore.Hook{usage, denialLimitHook{denials: denials, usage: usage}}, r.cfg.Hooks...)
	if sink != nil {
		hooks = append(hooks, toolEventHook{sink: sink, sessionID: result.SessionID, turnID: result.TurnID, runID: result.RunID})
	}
	return hooks
}

// appendTurnOutput replays the turn's tool results (with their call arguments)
// and the final answer into the session transcript.
func appendTurnOutput(ctx context.Context, transcript *Transcript, storage agentcore.Storage, sessionID, runID string, coreResult *agentcore.RunResult) {
	if messages, err := storage.LoadMessages(ctx, runID); err != nil {
		slog.Warn("read personal run transcript", "session_id", sessionID, "err", err)
	} else {
		// Tool arguments live on the assistant's ToolCall message; index them by call id
		// so each result can be summarized with its call parameters.
		calls := map[string]map[string]any{}
		for _, message := range messages {
			if message.ToolCall != nil {
				calls[message.ToolCall.CallID] = message.ToolCall.Arguments
			}
		}
		for _, message := range messages {
			if message.ToolResult == nil {
				continue
			}
			if err := transcript.AppendToolCall(message.ToolResult.Name, calls[message.ToolResult.CallID], message.ToolResult.Content); err != nil {
				slog.Warn("append personal transcript", "session_id", sessionID, "err", err)
			}
		}
	}
	if coreResult != nil {
		if err := transcript.AppendAssistant(coreResult.Output); err != nil {
			slog.Warn("append personal transcript", "session_id", sessionID, "err", err)
		}
	}
}

// markRunning flips the session to running with this turn as last_turn_id and
// returns the fresh session (metadata included).
func (r *Runner) markRunning(ctx context.Context, sessionID, turnID string) (Session, error) {
	fresh, err := r.cfg.Repository.GetSession(ctx, sessionID)
	if err != nil {
		return Session{}, err
	}
	fresh.Status, fresh.LastTurnID, fresh.UpdatedAt = store.SessionRunning, turnID, time.Now().UTC()
	if err := r.cfg.Repository.UpdateSession(ctx, fresh); err != nil {
		return fresh, err
	}
	return fresh, nil
}

// toolArgSummaryLimit caps the argument summary carried by "tool_call" events.
const toolArgSummaryLimit = 200

// toolEventHook publishes "tool_call" (before execution) and "tool_result"
// (after) events. Emit failures are only logged: returning them from a hook
// stage would abort the tool or the turn, and display must never do that.
type toolEventHook struct {
	agentcore.NopHook
	sink      EmitFunc
	sessionID string
	turnID    string
	runID     string
}

func (h toolEventHook) publish(ctx context.Context, event Event) {
	event.SessionID, event.TurnID, event.RunID = h.sessionID, h.turnID, h.runID
	if err := emit(ctx, h.sink, event); err != nil {
		slog.Warn("emit tool event", "session_id", h.sessionID, "kind", event.Kind, "tool", event.ToolName, "err", err)
	}
}

func (h toolEventHook) BeforeTool(ctx context.Context, event *agentcore.ToolEvent, _ *agentcore.Control) error {
	if event == nil || event.Call == nil {
		return nil
	}
	h.publish(ctx, Event{Kind: "tool_call", ToolName: event.Call.Name, Text: toolArgSummary(event.Call.Arguments)})
	return nil
}

func (h toolEventHook) AfterTool(ctx context.Context, event *agentcore.ToolEvent, _ *agentcore.Control) error {
	if event == nil || event.Result == nil {
		return nil
	}
	h.publish(ctx, Event{Kind: "tool_result", ToolName: event.Result.Name, Text: event.Result.Content, IsError: event.Result.IsError})
	return nil
}

// toolArgSummary renders call arguments as compact JSON, redacted and truncated.
func toolArgSummary(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	data, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return truncateRunes(redact.Secrets(string(data)), toolArgSummaryLimit)
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

func (r *Runner) findSession(ctx context.Context, req RunRequest) (Session, error) {
	if req.SessionID == "" {
		return r.NewSession(ctx, "", req.Prompt)
	}
	model, err := r.LoadSession(ctx, req.SessionID)
	if err == nil {
		return model, nil
	}
	if req.CreateIfMissing && errors.Is(err, ErrSessionNotFound) {
		if strings.TrimSpace(req.Prompt) == "" {
			return Session{}, fmt.Errorf("prompt is required to create session %q", req.SessionID)
		}
		return r.NewSession(ctx, req.SessionID, req.Prompt)
	}
	return Session{}, err
}

// withSessionUsage fills result.SessionUsage with the session-cumulative usage
// (model events of the session). Error returns that happen before finish use it
// too, so callers such as the ACP usage_update keep the previously known total
// instead of seeing a zero value. A lookup failure leaves the value untouched.
func (r *Runner) withSessionUsage(ctx context.Context, sessionID string, result RunResult) RunResult {
	// The caller's ctx may already be cancelled or timed out (lock wait).
	sum, err := r.cfg.Repository.SumUsage(context.WithoutCancel(ctx), sessionID)
	if err != nil {
		slog.Warn("sum session usage", "session_id", sessionID, "err", err)
		return result
	}
	result.SessionUsage = usageFromSum(sum)
	return result
}

func (r *Runner) finish(ctx context.Context, model Session, result RunResult, turnStart time.Time, usage *usageCollector, coreResult *agentcore.RunResult, runErr error, sink EmitFunc) (RunResult, error) {
	if coreResult != nil {
		result.Status, result.Output, result.Usage = coreResult.Status, coreResult.Output, coreResult.Usage
	}
	// Prefer the hook-collected sum: it also covers failed/cancelled turns.
	turnUsage, calls, lastInput := usage.snapshot()
	result.ModelCalls, result.LastInputTokens = calls, lastInput
	if calls > 0 {
		result.Usage = turnUsage
	}
	if coreResult == nil && runErr == nil {
		runErr = fmt.Errorf("runtime returned no result")
	}
	if runErr != nil {
		result.Status = agentcore.RunFailed
	}
	// An aborted approval (cancelled permission prompt) cancels the turn even
	// when the turn context itself is still live.
	canceled := ctx.Err() != nil || errors.Is(runErr, context.Canceled)
	if canceled {
		result.Status = agentcore.RunFailed
	}
	denials := turnDenialsFrom(ctx)
	result.DeniedTools, result.DenialLimitReached = denials.tools(), denials.limitReached()

	// A yielded turn is stored as completed (its messages become history for the
	// next turn) but reported to callers as INPUT_REQUIRED.
	runState, turnStatus, stopReason := RunCompleted, store.TurnCompleted, ""
	switch result.Status {
	case agentcore.RunCompleted:
	case agentcore.RunYielded:
		runState, stopReason = RunInputRequired, "input_required"
	case agentcore.RunFailed:
		if canceled {
			runState, turnStatus, stopReason = RunCanceled, store.TurnCancelled, "cancelled"
		} else {
			runState, turnStatus, stopReason = RunFailed, store.TurnFailed, "run_failed"
		}
	default:
		runState, turnStatus, stopReason = RunFailed, store.TurnFailed, "unexpected_status"
	}
	result.State = runState
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	now := time.Now().UTC()
	var persistErr error
	if err := r.cfg.Repository.UpdateTurn(finishCtx, store.Turn{ID: result.TurnID, Status: turnStatus, StopReason: stopReason, EndedAt: now}); err != nil {
		persistErr = errors.Join(persistErr, err)
	}
	// The turn event is written outside the entry transactions and is
	// best-effort; it is also what the trace timeline anchors on.
	turnAttrs := map[string]any{"stop_reason": stopReason, "status": string(result.Status)}
	if n := denials.count(); n > 0 {
		turnAttrs["denials"] = n
		turnAttrs["denial_limit_reached"] = result.DenialLimitReached
	}
	r.rec.record(finishCtx, store.Event{
		SessionID: model.ID, TurnID: result.TurnID, Kind: store.EventTurn, Name: "turn",
		Status: strings.ToLower(string(runState)), StartedAt: turnStart, DurationMs: time.Since(turnStart).Milliseconds(),
		Attrs: turnAttrs,
	})
	// Session-cumulative usage is derived from the model events of the session,
	// which already include this turn's calls (written synchronously above).
	result = r.withSessionUsage(finishCtx, model.ID, result)
	// Back to open with the current metadata (status columns only).
	if fresh, err := r.cfg.Repository.GetSession(finishCtx, model.ID); err != nil {
		persistErr = errors.Join(persistErr, err)
	} else {
		fresh.Status, fresh.UpdatedAt = store.SessionOpen, now
		if err := r.cfg.Repository.UpdateSession(finishCtx, fresh); err != nil {
			persistErr = errors.Join(persistErr, err)
		}
	}
	if err := emit(finishCtx, sink, Event{Kind: "run_finished", SessionID: result.SessionID, TurnID: result.TurnID, RunID: result.RunID, Status: result.Status, State: result.State, Text: result.Output}); err != nil {
		persistErr = errors.Join(persistErr, err)
	}
	return result, errors.Join(runErr, persistErr)
}

// maxStepsWrapUp is the request-only instruction used when a turn reaches its
// step limit: the model answers from what it has instead of the turn failing
// and losing the work done so far.
func maxStepsWrapUp(maxSteps int) string {
	if maxSteps <= 0 {
		return ""
	}
	return fmt.Sprintf("已达到本轮的步数上限（%d 步）。不要再调用任何工具，直接根据目前已获得的信息回答：说明已完成的部分、尚未完成的部分，以及继续时建议的下一步。", maxSteps)
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

type rulesPromptBuilder struct {
	base     agentcore.PromptBuilder
	metadata map[string]any
	// deferredTools is the stable deferred-tool listing ("" when none).
	deferredTools string
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
	if b.deferredTools != "" {
		parts = append(parts, b.deferredTools)
	}
	return strings.Join(parts, "\n\n"), nil
}
