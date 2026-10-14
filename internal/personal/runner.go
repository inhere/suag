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

	"github.com/inhere/suga/internal/personal/process"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/redact"
	"github.com/inhere/suga/pkg/agentcore/tools"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
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
	Repository   *store.Store
	Provider     agentcore.Provider
	Model        string
	ProviderName string
	Tools        agentcore.ToolRegistry
	// PromptBuilder is the stable system prompt (identity and working
	// conventions only, design D1); the runner sends it unchanged.
	PromptBuilder agentcore.PromptBuilder
	// RuleSource reads the user/project rules (content, hash). Rules go into
	// the segment context; a change inside a segment appends a context update.
	RuleSource func() (string, string, error)
	// EnvironmentSource and SkillsSource render the environment section (OS,
	// workspace, shell, read/write mode and how to enable writing) and the
	// skills listing. Both go into the segment context: read for every new
	// segment, and the environment is also compared at each turn start (a
	// resumed session in another mode or workspace gets a context update).
	EnvironmentSource func() string
	SkillsSource      func() string
	// SkillsStats counts the skills by how the listing presents them; the
	// context event records it.
	SkillsStats func() SkillsStats
	// Tasks hands over the notices of background tasks that ended unseen;
	// they are added before the next model request (nil: no tasks).
	Tasks              TaskNotices
	AppendSystemPrompt string
	// AppendSystemPromptSet marks AppendSystemPrompt as explicitly supplied by
	// the caller, so an empty value clears the saved prompt instead of keeping it.
	AppendSystemPromptSet bool
	Hooks                 []agentcore.Hook
	TranscriptDir         string
	LockRoot              string
	ProcessRoot           string
	MaxSteps              int
	// MaxRunTokens and RunTimeout ask a run to wrap up (answer from what it
	// has) past that many tokens (uncached input plus output) or that long;
	// 0 = no limit. Sub-agent runners set them.
	MaxRunTokens int
	RunTimeout   time.Duration
	// Now is the clock of the run time limit (RunTimeout): a turn's deadline is
	// Now()+RunTimeout and the limit hook checks Now() against it after every
	// model call. nil means time.Now. A test injects a clock because the check
	// needs the clock to have moved on, which the real one does not guarantee
	// within so short a limit (its resolution can exceed the limit).
	Now func() time.Time
	// ContextWindow is the effective model's context window in tokens (0 = 128000).
	ContextWindow int
	// CompactReserveTokens overrides the compaction reserve (default 16000).
	// A new segment keeps only the last complete tool exchange verbatim.
	CompactReserveTokens int
	// FoldEvery/FoldKeep configure history folding of the request view
	// (context.fold_every / context.fold_keep, see foldHistory); FoldEvery 0 = off.
	FoldEvery, FoldKeep int
	// CompactProvider/CompactModel summarize history when llm.compact_model is
	// set; nil means the turn's own provider is used.
	CompactProvider agentcore.Provider
	CompactModel    string
	// Plans is the update_plan sink handed to the tool at build time; New binds
	// it to this runner's store.
	Plans *PlanRecorder
	// Visibility is the deferred-tool layer already wrapped around Tools; New
	// binds it to this runner's store (loaded_tools meta) and event recorder,
	// and its tool list is added to the segment context.
	Visibility *tools.Visibility
	// Price looks up the configured price of a (provider id, model); nil or a
	// miss means cost stays 0 unless the provider reports it.
	Price func(provider, model string) (Price, bool)
	// InlineOutputBytes is the output layer's inline threshold
	// (tools.max_output_bytes; 0 = tools.DefaultInlineOutputBytes).
	InlineOutputBytes int
	// Spiller stores tool outputs above the threshold (e.g. a SessionSpiller);
	// nil means they are only truncated.
	Spiller tools.Spiller
	// MinimizeOutput tidies tool results before they enter the context
	// (tools.minimize_output, see tools.OutputConfig.Minimize); nil = off.
	MinimizeOutput func(call agentcore.ToolCall, content string) (string, bool)
	// PrefetchTools runs a reply's local read calls ahead concurrently
	// (tools.parallel_reads, see agentcore.Config.PrefetchTools); nil = off.
	PrefetchTools func(ctx context.Context, calls []agentcore.ToolCall)
	// Ledger backs session_search / recall; New binds it to this runner's
	// store (late binding, like Plans).
	Ledger *Ledger
	// TaintTools always taint the session when they return a result
	// (memory.taint_tools); TaintOnSpill taint it only when their output was
	// externalised (memory.taint_on_spill). Tools declaring External always
	// taint.
	TaintTools   []string
	TaintOnSpill []string
	// Mode is the run mode recorded in turn_context events: "read_only" or
	// "allow_write" (empty = unknown).
	Mode string
	// MemoryIndex returns the rendered memory indexes of every registered
	// scope (memory.Index.Render, concatenated). The runner freezes them in
	// the segment snapshot (re-read at every segment break) and renders them
	// after the rules; nil means no memory section. A failure is logged and
	// never affects the turn.
	MemoryIndex func(ctx context.Context) (string, error)
	// Version is the suga build version, recorded on each turn (turn_context)
	// and as the session's suga_version meta (the last version that ran it).
	Version string
	// Workspace is recorded as the cwd of the sessions this runner creates
	// (and of older sessions without one on their next turn); session_search
	// scope workspace matches sessions by it.
	Workspace string
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
	Plan *native.Plan
	// Usage is set on "usage" events (after each counted model call) and on
	// "run_finished" (the final run total).
	Usage *UsageUpdate
	// Retry is set on "retry" events: a model request failed for a transient
	// reason and is retried after Wait; Text is the reason.
	Retry *RetryInfo
	// Agent is set on "agent_progress" events (a sub-agent's tool call,
	// ToolName and Text as on "tool_call"): the sub-agent's description.
	Agent  string
	Status agentcore.RunStatus
	State  RunState
}

// RetryInfo describes a model request retry (see agentcore.StreamEventRetry).
type RetryInfo struct {
	// Attempt counts the failed attempts so far (1 for the first retry).
	Attempt int
	Wait    time.Duration
	// Restart: the failed attempt had already streamed text; the message is
	// streamed again from its start.
	Restart bool
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
	// RunUsage is the observe-layer run total: every model call of this run
	// (compaction and failed attempts that reported usage included) and the
	// tool calls (refused included). stream-json and ACP report it as "usage".
	RunUsage RunUsage
	// LastInputTokens is the input tokens (cache included) of the most recent
	// model call, i.e. the context size of the last request; 0 without a call.
	LastInputTokens int
	// DeniedTools lists the distinct tools refused by the approval layer in
	// this turn (first-refusal order); empty when nothing was refused.
	DeniedTools []string
	// DenialLimitReached is true when the turn was ended because DenialLimit
	// refusals accumulated (ACP maps it to stopReason refusal).
	DenialLimitReached bool
	// WrapUp is why the answer was a wrap-up ("max_steps", or the reason a
	// hook asked for one, e.g. a sub-agent's "max_tokens"); empty for a
	// normal answer.
	WrapUp string
}

// Runner owns one synchronous turn at a time for a persisted personal session.
type Runner struct {
	cfg       Config
	locksMu   sync.Mutex
	heldLocks map[string]*process.SessionLock
	// gateMu guards gates, the in-process single-flight gates by session
	// (see gate.go).
	gateMu sync.Mutex
	gates  map[string]*gateState
	rec    *recorder
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
	if cfg.Ledger != nil {
		cfg.Ledger.bind(cfg.Repository)
	}
	rec := &recorder{sink: cfg.Repository, price: cfg.Price,
		taint:  newTaintMarker(cfg.Repository, cfg.TaintTools, cfg.TaintOnSpill),
		prefix: newPrefixTracker(), workspace: cfg.Workspace}
	cfg.Provider = rec.observeProvider(cfg.Provider, cfg.ProviderName, cfg.Model)
	if cfg.Tools != nil {
		// Stack: observe -> output -> (visibility -> approval -> base). The
		// output layer sits inside observe so tool events see the shortened
		// result and its OutputInfo.
		cfg.Tools = rec.observeTools(tools.NewOutput(cfg.Tools, tools.OutputConfig{
			InlineBytes: cfg.InlineOutputBytes, Spiller: cfg.Spiller, Sanitize: redact.Secrets,
			Minimize: cfg.MinimizeOutput,
		}))
	}
	if cfg.CompactProvider != nil {
		cfg.CompactProvider = rec.observeProvider(cfg.CompactProvider, cfg.ProviderName, cfg.CompactModel)
	}
	runner := &Runner{cfg: cfg, heldLocks: make(map[string]*process.SessionLock), rec: rec}
	runner.bindVisibility()
	return runner, nil
}

func (r *Runner) ProcessRoot() string { return r.cfg.ProcessRoot }

// ProviderName and Model identify the provider config id and model this runner
// calls; chat shows them in its per-turn status line.
func (r *Runner) ProviderName() string { return r.cfg.ProviderName }
func (r *Runner) Model() string        { return r.cfg.Model }

// Fold returns the history folding settings (every, keep); every 0 = off.
func (r *Runner) Fold() (every, keep int) { return r.cfg.FoldEvery, r.cfg.FoldKeep }

func (r *Runner) NewSession(ctx context.Context, id, title string) (Session, error) {
	return r.NewSessionWithMeta(ctx, id, title, nil)
}

// NewSessionWithMeta is NewSession with extra meta stored as the session is
// created (a sub-agent session names its parent from the start, so it is
// never listed as a session of its own, not even after a crash).
func (r *Runner) NewSessionWithMeta(ctx context.Context, id, title string, extra map[string]any) (Session, error) {
	if id == "" {
		id = NewSessionID()
	}
	if err := validateSessionID(id); err != nil {
		return Session{}, err
	}
	now := time.Now().UTC()
	model := Session{
		ID:        id,
		Title:     SessionTitle(title),
		Cwd:       r.cfg.Workspace,
		Status:    store.SessionOpen,
		CreatedAt: now,
		UpdatedAt: now,
		Meta:      map[string]any{},
	}
	if model.Title == "" {
		model.Title = defaultTitle(id)
	}
	// The first segment's snapshot (design D2). A RuleSource error fails the
	// session; a memory index that cannot be read leaves the snapshot without
	// one and the first turn that can read it appends a context update.
	seg, err := r.freshSnapshot(ctx, id, nil)
	if err != nil {
		return Session{}, err
	}
	for key, value := range segmentMeta(seg) {
		model.Meta[key] = value
	}
	model.Meta["model"] = r.cfg.Model
	model.Meta["provider"] = r.cfg.ProviderName
	for key, value := range extra {
		model.Meta[key] = value
	}
	if err := r.cfg.Repository.CreateSession(ctx, model); err != nil {
		return Session{}, fmt.Errorf("create personal session: %w", err)
	}
	// Every new session starts with a segment start record freezing the
	// snapshot above (design D2). Without it the session is not created.
	if err := r.writeStartSegment(ctx, model.ID, seg); err != nil {
		_ = r.cfg.Repository.DeleteSession(context.WithoutCancel(ctx), model.ID)
		return Session{}, err
	}
	return model, nil
}

// SetSessionTitle updates the persisted display title after an interactive chat's first prompt.
func (r *Runner) SetSessionTitle(ctx context.Context, sessionID, title string) error {
	model, err := r.LoadSession(ctx, sessionID)
	if err != nil {
		return err
	}
	model.Title = SessionTitle(title)
	model.UpdatedAt = time.Now().UTC()
	if err := r.cfg.Repository.UpdateSessionFields(ctx, model); err != nil {
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
	// Session single flight (design D5): another Run or a :compact of this
	// session in this process finishes first, an idle consolidation is
	// preempted. Then re-read the session the previous holder may have changed.
	release, err := r.gate(model.ID).Acquire(ctx)
	if err != nil {
		return r.withSessionUsage(ctx, model.ID, RunResult{SessionID: model.ID}), err
	}
	defer release()
	fresh, err := r.LoadSession(ctx, model.ID)
	if err != nil {
		return r.withSessionUsage(ctx, model.ID, RunResult{SessionID: model.ID}), err
	}
	model = fresh
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
	ctx = context.WithValue(ctx, turnInputKey{}, turnInput{chars: len(req.Prompt), injected: req.Injected})
	// Refused tool calls are counted per turn in ctx (see denial.go).
	ctx, denials := withTurnDenials(ctx)
	// The run-scoped usage is counted at the observe layer and published after
	// every model call (design D8).
	meter := r.newRunUsageMeter(ctx, model.ID, turnID, req.Emit)
	ctx = withRunUsage(ctx, meter)
	// Sub-agents of this session count their usage in this run (usage.go).
	defer activateMeter(model.ID, meter)()
	result := RunResult{SessionID: model.ID, TurnID: turnID, RunID: turnID}
	history, err := r.loadHistory(ctx, model.ID)
	if err != nil {
		return r.withSessionUsage(ctx, model.ID, result), err
	}
	// The segment in effect comes from the adopted records only (never from
	// meta, which a failed turn's mid-turn break may already have moved).
	r.resolveSegment(&history, model.Meta)
	// Rules/append prompt/environment changed inside the segment (or the
	// memory index is still missing): this turn appends a context update.
	update, err := r.contextUpdate(ctx, model.ID, history.eff)
	if err != nil {
		return r.withSessionUsage(ctx, model.ID, result), err
	}

	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		agentID = "suga"
	}
	if err := r.cfg.Repository.CreateTurn(ctx, store.Turn{ID: turnID, SessionID: model.ID, Status: store.TurnRunning}); err != nil {
		return r.withSessionUsage(ctx, model.ID, result), fmt.Errorf("create turn: %w", err)
	}
	// Collect this turn's per-call usage (the session total comes from model events).
	usage := &usageCollector{cost: func(u agentcore.Usage) float64 {
		return costOf(r.cfg.Price, r.cfg.ProviderName, r.cfg.Model, u)
	}}
	// Keep session metadata aligned with the provider selected for this turn.
	metaPatch := map[string]any{"model": r.cfg.Model, "provider": r.cfg.ProviderName}
	if r.cfg.Version != "" {
		metaPatch[metaSugaVersion] = r.cfg.Version
	}
	if err := r.cfg.Repository.PatchSessionMeta(ctx, model.ID, metaPatch); err != nil {
		return r.finish(ctx, model, result, turnStart, usage, nil, err, req.Emit)
	}
	// Re-read so the prompt builder and the status write see the patched,
	// current metadata (including usage written by another process).
	model, err = r.markRunning(ctx, model.ID, turnID)
	if err != nil {
		return r.finish(ctx, model, result, turnStart, usage, nil, err, req.Emit)
	}
	// The update is an entry of this turn written before the turn's user
	// message, so it is adopted with the turn and replayed at the same place
	// (a ctxItem: stable position, a cut candidate, never in state.Messages).
	if update != nil {
		item, err := r.writeContextUpdate(ctx, model.ID, turnID, update)
		if err != nil {
			return r.finish(ctx, model, result, turnStart, usage, nil, err, req.Emit)
		}
		history.items = append(history.items, item)
		update.applyTo(history.eff)
	}
	// What this turn starts with (D9); best-effort like every event.
	r.recordTurnContext(ctx, model, history.segment, history.eff)
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
			Retry:     retryInfoOf(update),
		})
	}
	// update_plan reaches the caller through this turn's Emit callback.
	ctx = withPlanEmitter(ctx, func(ctx context.Context, plan native.Plan) error {
		return emit(ctx, req.Emit, Event{Kind: "plan", SessionID: result.SessionID, TurnID: result.TurnID, RunID: result.RunID, Plan: &plan})
	})
	// So does a sub-agent's progress (AgentProgress; subagent.go).
	if req.Emit != nil {
		ctx = context.WithValue(ctx, agentEmitKey{}, EmitFunc(func(ctx context.Context, e Event) error {
			e.SessionID, e.TurnID, e.RunID = result.SessionID, result.TurnID, result.RunID
			return emit(ctx, req.Emit, e)
		}))
	}
	var storage agentcore.Storage = r.cfg.Repository.AgentStorage()
	if req.Injected {
		// Persist the existing "not typed by a person" marker on the turn's
		// request, so a later handoff package can tell it apart (D4).
		storage = &injectedStorage{Storage: storage}
	}
	promptBuilder := stablePromptBuilder{base: r.cfg.PromptBuilder}
	// The hook context (req.SystemContext) is a user message of this turn.
	contextBuilder := r.newContextBuilder(model.ID, turnID, history, promptBuilder, usage, req.SystemContext)
	// Fold events already recorded in this segment (an earlier turn's builder).
	contextBuilder.foldBase = metaCount(model.Meta[metaFoldBoundary])
	runtimeHooks := r.turnHooks(usage, denials, req.Emit, result)
	runtime := agentcore.New(agentcore.Config{
		Provider:       r.cfg.Provider,
		Storage:        storage,
		Tools:          r.cfg.Tools,
		PromptBuilder:  promptBuilder,
		ContextBuilder: contextBuilder,
		Hooks:          runtimeHooks,
		MaxSteps:       r.cfg.MaxSteps,
		MaxStepsWrapUp: maxStepsWrapUp(r.cfg.MaxSteps),
		// Refused calls become tool results; aborted approvals end the turn.
		ToolExecutionPolicy: abortingToolPolicy{},
		PrefetchTools:       r.cfg.PrefetchTools,
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

// turnHooks assembles the runtime hooks of one turn. The collector goes first
// so a later hook short-circuiting the chain cannot hide a model call from the
// usage total. The denial limit hook ends the turn before the next model call
// once DenialLimit refusals accumulated.
func (r *Runner) turnHooks(usage *usageCollector, denials *turnDenials, sink EmitFunc, result RunResult) []agentcore.Hook {
	hooks := append([]agentcore.Hook{usage, denialLimitHook{denials: denials, usage: usage},
		prefixHook{tracker: r.rec.prefix}}, r.cfg.Hooks...)
	if r.cfg.MaxRunTokens > 0 || r.cfg.RunTimeout > 0 {
		limits := limitHook{maxTokens: r.cfg.MaxRunTokens, now: r.clock()}
		if r.cfg.RunTimeout > 0 {
			limits.deadline = limits.now().Add(r.cfg.RunTimeout)
		}
		hooks = append(hooks, limits)
	}
	if sink != nil {
		hooks = append(hooks, toolEventHook{sink: sink, sessionID: result.SessionID, turnID: result.TurnID, runID: result.RunID})
	}
	return hooks
}

// clock is the time source of the run time limit (Config.Now; the real clock
// when none was injected).
func (r *Runner) clock() func() time.Time {
	if r.cfg.Now != nil {
		return r.cfg.Now
	}
	return time.Now
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
	// Sessions created before cwd was recorded pick it up on their next turn.
	if fresh.Cwd == "" {
		fresh.Cwd = r.cfg.Workspace
	}
	if err := r.cfg.Repository.UpdateSessionFields(ctx, fresh); err != nil {
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

func (r *Runner) findSession(ctx context.Context, req RunRequest) (Session, error) {
	if req.SessionID == "" {
		return r.NewSession(ctx, "", req.Prompt)
	}
	model, err := r.LoadSession(ctx, req.SessionID)
	if err == nil {
		// A session created without a title (ACP session/new) takes its
		// first prompt's.
		if model.Title == defaultTitle(model.ID) && !req.Injected {
			if title := SessionTitle(req.Prompt); title != "" {
				if err := r.SetSessionTitle(ctx, model.ID, title); err != nil {
					return Session{}, err
				}
				model.Title = title
			}
		}
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
// (model events of the session and of its sub-agent sessions). Error returns that happen before finish use it
// too, so callers such as the ACP usage_update keep the previously known total
// instead of seeing a zero value. A lookup failure leaves the value untouched.
func (r *Runner) withSessionUsage(ctx context.Context, sessionID string, result RunResult) RunResult {
	// The caller's ctx may already be cancelled or timed out (lock wait).
	sum, err := r.cfg.Repository.SumUsageTree(context.WithoutCancel(ctx), sessionID)
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
		result.WrapUp = coreResult.WrapUp
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
	// What started the turn: the input's length and whether a person typed
	// it (injected = a hook reply, gofer delivery, ...).
	if in, ok := ctx.Value(turnInputKey{}).(turnInput); ok {
		turnAttrs["prompt_chars"] = in.chars
		if in.injected {
			turnAttrs["injected"] = true
		}
	}
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
	result.RunUsage = runUsageFrom(ctx).snapshot()
	// Back to open with the current metadata (status columns only).
	if fresh, err := r.cfg.Repository.GetSession(finishCtx, model.ID); err != nil {
		persistErr = errors.Join(persistErr, err)
	} else {
		fresh.Status, fresh.UpdatedAt = store.SessionOpen, now
		if err := r.cfg.Repository.UpdateSessionFields(finishCtx, fresh); err != nil {
			persistErr = errors.Join(persistErr, err)
		}
	}
	final := &UsageUpdate{Run: result.RunUsage, Session: result.SessionUsage}
	if err := emit(finishCtx, sink, Event{Kind: "run_finished", SessionID: result.SessionID, TurnID: result.TurnID, RunID: result.RunID, Status: result.Status, State: result.State, Text: result.Output, Usage: final}); err != nil {
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

// turnInput is the turn's input as seen by finish (the turn event).
type turnInput struct {
	chars    int
	injected bool
}

type turnInputKey struct{}

// metaSugaVersion is the session meta key of the suga version that last ran
// a turn of it.
const metaSugaVersion = "suga_version"

// retryInfoOf reads a StreamEventRetry's metadata; nil for other events.
func retryInfoOf(ev agentcore.StreamEvent) *RetryInfo {
	if ev.Kind != agentcore.StreamEventRetry {
		return nil
	}
	info := &RetryInfo{}
	info.Attempt, _ = ev.Metadata["attempt"].(int)
	if ms, ok := ev.Metadata["wait_ms"].(int64); ok {
		info.Wait = time.Duration(ms) * time.Millisecond
	}
	info.Restart, _ = ev.Metadata["restart"].(bool)
	return info
}
