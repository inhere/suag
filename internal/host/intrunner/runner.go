package intrunner

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	agentcorestorage "github.com/inhere/axon/internal/acstore"
	"github.com/inhere/axon/internal/agent"
	"github.com/inhere/axon/internal/host/session"
	"github.com/inhere/axon/internal/ops/hitl"
	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/internal/platform/memory"
	"github.com/inhere/axon/internal/platform/tools"
	"github.com/inhere/axon/internal/platform/tracing"
	"github.com/inhere/axon/pkg/agentcore"
)

type Request = session.RuntimeRequest
type Result = session.RuntimeResult
type Event = session.RuntimeEvent

// Config defines the narrow dependencies needed by the host internal runner.
type Config struct {
	Provider      llm.Provider
	Registry      *tools.Registry
	Tracer        tracing.Tracer
	HITL          hitl.Manager
	Hooks         []agentcore.Hook
	RunRoot       string
	MemoryRoot    string
	Runtime       string
	FailThreshold int
	MaxSteps      int
}

type sessionState struct {
	history      []memory.Message
	storage      *agentcorestorage.SQLiteStorage
	agent        *agent.Agent
	runner       agent.Runner
	loopPhase    string
	resumeSource string
	resumeCount  int
}

type sessionMemory struct {
	mu         sync.Mutex
	recalled   []memory.Message
	history    []memory.Message
	transcript []memory.Message
}

// SessionRunner adapts the internal single-agent runtime to the host session.Runner contract.
// It keeps session-scoped memory/storage so follow-up turns can continue with local context.
type SessionRunner struct {
	cfg      Config
	mu       sync.Mutex
	sessions map[string]*sessionState
}

var errResumeStateUnavailable = fmt.Errorf("waiting runtime is no longer available; start a new turn or reissue the task")

// New creates a host runner backed by the internal agent runtimes.
func New(cfg Config) (*SessionRunner, error) {
	if cfg.Provider == nil {
		return nil, fmt.Errorf("internal host runner requires llm provider")
	}
	if cfg.Registry == nil {
		cfg.Registry = tools.NewRegistry()
	}
	if cfg.Tracer == nil {
		cfg.Tracer = tracing.NewInMemoryTracer(nil)
	}
	if strings.TrimSpace(cfg.RunRoot) == "" {
		cfg.RunRoot = filepath.Join(".agentcore", "host")
	}

	return &SessionRunner{
		cfg:      cfg,
		sessions: make(map[string]*sessionState),
	}, nil
}

// Run executes a host session turn on the configured internal runtime.
func (r *SessionRunner) Run(ctx context.Context, in session.RuntimeRequest) (session.RuntimeResult, error) {
	if r == nil {
		return session.RuntimeResult{}, fmt.Errorf("internal host runner is nil")
	}

	state, err := r.getOrCreateSessionState(in.SessionID)
	if err != nil {
		return session.RuntimeResult{}, err
	}
	if err := emitStatus(in.Emit, session.RunPreparing); err != nil {
		return session.RuntimeResult{}, err
	}

	recalled := newFormalMemoryRecall(r.cfg.MemoryRoot).Load(in.Instruction)
	runMemory := newSessionMemory(recalled.Messages, state.history)
	a, runner, err := r.resolveRuntime(state, in, runMemory)
	if err != nil {
		return session.RuntimeResult{}, err
	}
	if err := emitStatus(in.Emit, session.RunThinking); err != nil {
		return session.RuntimeResult{}, err
	}

	result, err := runner.Run(ctx, &agent.Task{
		ID:          strings.TrimSpace(in.RunID),
		Title:       "host session run",
		Description: strings.TrimSpace(in.Instruction),
		AssignedTo:  a.ID,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	})
	r.storeHistory(in.SessionID, runMemory.Snapshot())
	if err != nil {
		r.storeActiveRuntime(in.SessionID, a, runner)
		return session.RuntimeResult{}, err
	}

	summary := ""
	if result != nil {
		summary = strings.TrimSpace(result.Summary)
		if summary == "" {
			summary = strings.TrimSpace(result.Output)
		}
	}

	// Current internal yields are effectively human-input waits, so surface them
	// as host waiting states. We derive a coarse host-facing wait bucket from the
	// yielded summary/reason so web + gateway can distinguish approval/auth flows.
	if runner.GetState() == agent.AgentSuspended {
		waitMetadata := cloneWaitMetadata(resultMetadata(result))
		waitMetadata = mergeLoopMetadata(waitMetadata, r.loopMetadata(in.SessionID))
		waitState := deriveWaitState(summary, waitMetadata)
		if err := emitStatusWithMetadata(in.Emit, waitState, waitMetadata); err != nil {
			return session.RuntimeResult{}, err
		}
		r.storeActiveRuntime(in.SessionID, a, runner)
		return session.RuntimeResult{
			Summary:      summary,
			WaitState:    waitState,
			WaitMetadata: waitMetadata,
			Metadata:     cloneWaitMetadata(recalled.Metadata),
		}, nil
	}
	r.clearActiveRuntime(in.SessionID)

	return session.RuntimeResult{
		Summary:  summary,
		Metadata: cloneWaitMetadata(recalled.Metadata),
	}, nil
}

// Close releases per-session storage handles owned by the runner.
func (r *SessionRunner) Close() error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for key, state := range r.sessions {
		if state == nil || state.storage == nil {
			delete(r.sessions, key)
			continue
		}
		if err := state.storage.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(r.sessions, key)
	}
	return firstErr
}

func (r *SessionRunner) newAgent(agentID string, mem memory.Store) *agent.Agent {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		agentID = "default"
	}
	return &agent.Agent{
		ID:        agentID,
		Name:      agentID,
		Soul:      &agent.Soul{Name: agentID},
		State:     agent.AgentPending,
		Memory:    mem,
		Tools:     r.cfg.Registry,
		LLM:       r.cfg.Provider,
		CreatedAt: time.Now().UTC(),
	}
}

func (r *SessionRunner) getOrCreateSessionState(sessionID string) (*sessionState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if state := r.sessions[sessionID]; state != nil {
		return state, nil
	}

	storagePath := filepath.Join(r.cfg.RunRoot, safeSegment(sessionID), "runs.db")
	storage, err := agentcorestorage.NewSQLiteStorage(storagePath)
	if err != nil {
		return nil, fmt.Errorf("open agentcore storage for session %q: %w", sessionID, err)
	}

	state := &sessionState{
		storage: storage,
	}
	r.sessions[sessionID] = state
	return state, nil
}

func (r *SessionRunner) storeHistory(sessionID string, messages []memory.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.sessions[sessionID]
	if state == nil {
		return
	}
	state.history = cloneMemoryMessages(messages)
}

func (r *SessionRunner) storeActiveRuntime(sessionID string, a *agent.Agent, runner agent.Runner) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.sessions[sessionID]
	if state == nil {
		return
	}
	state.agent = a
	state.runner = runner
}

func (r *SessionRunner) clearActiveRuntime(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.sessions[sessionID]
	if state == nil {
		return
	}
	state.agent = nil
	state.runner = nil
	state.loopPhase = ""
	state.resumeSource = ""
	state.resumeCount = 0
}

func (r *SessionRunner) runtimeKind() agent.RuntimeKind {
	switch strings.ToLower(strings.TrimSpace(r.cfg.Runtime)) {
	case "react":
		// Host v0 keeps React only as an explicit legacy fallback for local
		// debugging/parity. The recommended internal runner path is agentcore.
		return agent.RuntimeReact
	default:
		return agent.RuntimeAgentcore
	}
}

func (r *SessionRunner) resolveRuntime(
	state *sessionState,
	in session.RuntimeRequest,
	runMemory *sessionMemory,
) (*agent.Agent, agent.Runner, error) {
	if state != nil && state.agent != nil && state.runner != nil && state.runner.GetState() == agent.AgentSuspended {
		resumeInstruction := strings.TrimSpace(in.Instruction)
		resumeSource := "resume_from_state"
		if resumeInstruction != "" {
			resumeSource = "resume_with_input"
		}
		if err := state.runner.Resume(resumeInstruction); err != nil {
			return nil, nil, fmt.Errorf("resume suspended runner: %w", err)
		}
		r.setLoopContext(in.SessionID, "resumed", resumeSource, state.resumeCount+1)
		return state.agent, state.runner, nil
	}

	a := r.newAgent(in.AgentID, runMemory)
	runnerCfg := agent.RunnerFactoryConfig{
		RuntimeKind:   r.runtimeKind(),
		Storage:       state.storage,
		Tracer:        r.cfg.Tracer,
		HITL:          r.cfg.HITL,
		FailThreshold: r.cfg.FailThreshold,
		MaxSteps:      r.cfg.MaxSteps,
		Hooks:         r.cfg.Hooks,
	}
	runner, err := agent.NewRunner(a, runnerCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("build internal agent runner: %w", err)
	}
	r.setLoopContext(in.SessionID, "initial", "", 0)
	return a, runner, nil
}

// ValidateContinue lets the host reject historical waiting runs that lost their
// in-memory suspended runtime, instead of creating a new continue turn that can
// never actually resume the previous execution.
func (r *SessionRunner) ValidateContinue(_ context.Context, in session.RuntimeRequest) error {
	if r == nil {
		return errResumeStateUnavailable
	}

	resumeTurnID, _ := in.Context["resume_turn_id"].(string)
	resumeFromRunID, _ := in.Context["resume_from_run_id"].(string)
	if strings.TrimSpace(resumeTurnID) == "" && strings.TrimSpace(resumeFromRunID) == "" {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.sessions[in.SessionID]
	if state == nil || state.agent == nil || state.runner == nil || state.runner.GetState() != agent.AgentSuspended {
		return errResumeStateUnavailable
	}
	return nil
}

func emitStatus(emit func(event session.RuntimeEvent) error, state session.RunState) error {
	return emitStatusWithMetadata(emit, state, nil)
}

func emitStatusWithMetadata(emit func(event session.RuntimeEvent) error, state session.RunState, metadata map[string]any) error {
	if emit == nil {
		return nil
	}
	payload := map[string]any{
		"state": string(state),
	}
	if len(metadata) > 0 {
		payload["waitMetadata"] = cloneWaitMetadata(metadata)
	}
	return emit(session.RuntimeEvent{
		EventType: session.EventRunStatus,
		Payload:   payload,
	})
}

func safeSegment(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "default"
	}
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"..", "_",
		" ", "_",
	)
	cleaned := replacer.Replace(raw)
	if cleaned == "" {
		return "default"
	}
	return cleaned
}

func cloneMemoryMessages(messages []memory.Message) []memory.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]memory.Message, len(messages))
	copy(out, messages)
	return out
}

func deriveWaitState(summary string, metadata map[string]any) session.RunState {
	switch normalizeWaitKind(metadata) {
	case "auth_required":
		return session.RunAuthRequired
	case "input_required":
		return session.RunInputRequired
	}

	reason := strings.ToLower(strings.TrimSpace(summary))
	if reason == "" {
		return session.RunInputRequired
	}
	if strings.Contains(reason, "auth") ||
		strings.Contains(reason, "approval") ||
		strings.Contains(reason, "authorize") {
		return session.RunAuthRequired
	}
	return session.RunInputRequired
}

func resultMetadata(result *agent.TaskResult) map[string]any {
	if result == nil || len(result.Metadata) == 0 {
		return nil
	}
	return result.Metadata
}

func normalizeWaitKind(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	raw, _ := metadata["wait_kind"].(string)
	return strings.ToLower(strings.TrimSpace(raw))
}

func cloneWaitMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]any, len(metadata))
	for key, value := range metadata {
		out[key] = value
	}
	return out
}

func mergeLoopMetadata(metadata map[string]any, loop map[string]any) map[string]any {
	if len(loop) == 0 {
		return metadata
	}
	out := cloneWaitMetadata(metadata)
	if out == nil {
		out = make(map[string]any, len(loop))
	}
	for key, value := range loop {
		out[key] = value
	}
	return out
}

func (r *SessionRunner) setLoopContext(sessionID, phase, resumeSource string, resumeCount int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.sessions[sessionID]
	if state == nil {
		return
	}
	state.loopPhase = strings.TrimSpace(phase)
	state.resumeSource = strings.TrimSpace(resumeSource)
	state.resumeCount = resumeCount
}

func (r *SessionRunner) loopMetadata(sessionID string) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.sessions[sessionID]
	if state == nil {
		return nil
	}

	metadata := map[string]any{
		"resume_count": state.resumeCount,
	}
	if phase := strings.TrimSpace(state.loopPhase); phase != "" {
		metadata["loop_phase"] = phase
	}
	if source := strings.TrimSpace(state.resumeSource); source != "" {
		metadata["resume_source"] = source
	}
	return metadata
}

func newSessionMemory(recalled []memory.Message, history []memory.Message) *sessionMemory {
	return &sessionMemory{
		recalled: cloneMemoryMessages(recalled),
		history:  cloneMemoryMessages(history),
	}
}

func (m *sessionMemory) AddMessage(role, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transcript = append(m.transcript, memory.Message{
		Role:      role,
		Content:   content,
		Timestamp: time.Now().UTC(),
	})
}

func (m *sessionMemory) GetMessages() []memory.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneMemoryMessages(m.transcript)
}

func (m *sessionMemory) GetContext(_ context.Context, _ int) ([]memory.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]memory.Message, 0, len(m.recalled)+len(m.history)+len(m.transcript))
	out = append(out, cloneMemoryMessages(m.recalled)...)
	out = append(out, cloneMemoryMessages(m.history)...)
	out = append(out, cloneMemoryMessages(m.transcript)...)
	return out, nil
}

func (m *sessionMemory) SaveFragment(context.Context, string, string) error {
	return nil
}

func (m *sessionMemory) SearchSimilar(context.Context, string, int) ([]memory.Fragment, error) {
	return nil, nil
}

func (m *sessionMemory) Close() error {
	return nil
}

func (m *sessionMemory) Snapshot() []memory.Message {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Recalled formal memory is runtime-only context and should not be written
	// back into session history, otherwise each turn would duplicate prior recall.
	out := make([]memory.Message, 0, len(m.history)+len(m.transcript))
	out = append(out, cloneMemoryMessages(m.history)...)
	out = append(out, cloneMemoryMessages(m.transcript)...)
	return out
}
