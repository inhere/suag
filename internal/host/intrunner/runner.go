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
	RunRoot       string
	Runtime       string
	FailThreshold int
	MaxSteps      int
}

type sessionState struct {
	history []memory.Message
	storage *agentcorestorage.SQLiteStorage
}

type sessionMemory struct {
	mu        sync.Mutex
	history   []memory.Message
	transcript []memory.Message
}

// SessionRunner adapts the internal single-agent runtime to the host session.Runner contract.
// It keeps session-scoped memory/storage so follow-up turns can continue with local context.
type SessionRunner struct {
	cfg      Config
	mu       sync.Mutex
	sessions map[string]*sessionState
}

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

	runMemory := newSessionMemory(state.history)
	a := r.newAgent(in.AgentID, runMemory)
	runnerCfg := agent.RunnerFactoryConfig{
		RuntimeKind:   r.runtimeKind(),
		Storage:       state.storage,
		Tracer:        r.cfg.Tracer,
		HITL:          r.cfg.HITL,
		FailThreshold: r.cfg.FailThreshold,
		MaxSteps:      r.cfg.MaxSteps,
	}
	runner, err := agent.NewRunner(a, runnerCfg)
	if err != nil {
		return session.RuntimeResult{}, fmt.Errorf("build internal agent runner: %w", err)
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
	// as host waiting states until finer-grained yield reasons land.
	if runner.GetState() == agent.AgentSuspended {
		if err := emitStatus(in.Emit, session.RunInputRequired); err != nil {
			return session.RuntimeResult{}, err
		}
	}

	return session.RuntimeResult{
		Summary: summary,
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

func (r *SessionRunner) runtimeKind() agent.RuntimeKind {
	switch strings.ToLower(strings.TrimSpace(r.cfg.Runtime)) {
	case "react":
		return agent.RuntimeReact
	default:
		return agent.RuntimeAgentcore
	}
}

func emitStatus(emit func(event session.RuntimeEvent) error, state session.RunState) error {
	if emit == nil {
		return nil
	}
	return emit(session.RuntimeEvent{
		EventType: session.EventRunStatus,
		Payload: map[string]any{
			"state": string(state),
		},
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

func newSessionMemory(history []memory.Message) *sessionMemory {
	return &sessionMemory{
		history: cloneMemoryMessages(history),
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

	out := make([]memory.Message, 0, len(m.history)+len(m.transcript))
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

	out := make([]memory.Message, 0, len(m.history)+len(m.transcript))
	out = append(out, cloneMemoryMessages(m.history)...)
	out = append(out, cloneMemoryMessages(m.transcript)...)
	return out
}
