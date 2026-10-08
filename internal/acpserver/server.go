package acpserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/acp-go-sdk"
	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/pkg/agentcore"
)

// Runtime is one opening of the shared personal session store and Agent core.
type Runtime struct {
	Runner *personal.Runner
	Close  func() error
	// ContextWindow is the model context size in tokens; 0 uses the configured
	// llm.context_window or a model-based default.
	ContextWindow int
}

func (r Runtime) close() error {
	if r.Close != nil {
		return r.Close()
	}
	return nil
}

// OpenRuntime assembles a turn with the selected workspace, tool mode and hook.
type OpenRuntime func(context.Context, string, bool, []appconfig.MCPServerSettings, agentcore.Hook) (Runtime, error)

type sessionState struct {
	cwd     string
	servers []appconfig.MCPServerSettings
	mode    acp.SessionModeId
	cancel  context.CancelFunc
	// total and lastContext are guarded by Agent.mu; see recordUsage.
	total       agentcore.Usage
	lastContext int
}

// Agent owns ACP connection state; personal.Runner owns persisted conversation state.
type Agent struct {
	mu       sync.Mutex
	conn     *acp.AgentSideConnection
	sessions map[acp.SessionId]*sessionState
	open     OpenRuntime
}

var _ acp.Agent = (*Agent)(nil)
var _ acp.AgentLoader = (*Agent)(nil)

func New(open OpenRuntime) *Agent {
	return &Agent{open: open, sessions: make(map[acp.SessionId]*sessionState)}
}

// SetConnection runs after the connection has started serving requests, so conn
// is guarded by mu like the rest of the shared state.
func (a *Agent) SetConnection(conn *acp.AgentSideConnection) {
	a.mu.Lock()
	a.conn = conn
	a.mu.Unlock()
}

func (a *Agent) connection() *acp.AgentSideConnection {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.conn
}

// Serve uses stdout exclusively for ACP JSON-RPC frames.
func Serve(ctx context.Context, input io.Reader, output io.Writer, open OpenRuntime) error {
	if open == nil {
		return fmt.Errorf("ACP runtime factory is required")
	}
	agent := New(open)
	conn := acp.NewAgentSideConnection(agent, output, input)
	agent.SetConnection(conn)
	select {
	case <-ctx.Done():
		return nil
	case <-conn.Done():
		return nil
	}
}

func (*Agent) Initialize(_ context.Context, req acp.InitializeRequest) (acp.InitializeResponse, error) {
	if req.ProtocolVersion != acp.ProtocolVersionNumber {
		return acp.InitializeResponse{}, acp.NewInvalidParams(map[string]any{"protocolVersion": req.ProtocolVersion})
	}
	return acp.InitializeResponse{
		ProtocolVersion:   acp.ProtocolVersionNumber,
		AgentCapabilities: acp.AgentCapabilities{LoadSession: true},
		AuthMethods:       []acp.AuthMethod{},
	}, nil
}

func (a *Agent) NewSession(ctx context.Context, req acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	cwd, servers, err := sessionInputs(req.Cwd, req.AdditionalDirectories, req.McpServers)
	if err != nil {
		return acp.NewSessionResponse{}, err
	}
	runtime, err := a.open(ctx, cwd, true, nil, nil)
	if err != nil {
		return acp.NewSessionResponse{}, err
	}
	model, runErr := runtime.Runner.NewSession(ctx, "", "")
	if err := errors.Join(runErr, runtime.close()); err != nil {
		return acp.NewSessionResponse{}, err
	}
	id := acp.SessionId(model.SessionID)
	a.mu.Lock()
	a.sessions[id] = &sessionState{cwd: cwd, servers: servers, mode: "default"}
	a.mu.Unlock()
	return acp.NewSessionResponse{SessionId: id, Modes: sessionModes("default")}, nil
}

func (a *Agent) LoadSession(ctx context.Context, req acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	cwd, servers, err := sessionInputs(req.Cwd, req.AdditionalDirectories, req.McpServers)
	if err != nil {
		return acp.LoadSessionResponse{}, err
	}
	runtime, err := a.open(ctx, cwd, true, nil, nil)
	if err != nil {
		return acp.LoadSessionResponse{}, err
	}
	_, loadErr := runtime.Runner.LoadSession(ctx, string(req.SessionId))
	closeErr := runtime.close()
	if loadErr != nil {
		return acp.LoadSessionResponse{}, acp.NewInvalidParams(map[string]any{"sessionId": req.SessionId})
	}
	if closeErr != nil {
		return acp.LoadSessionResponse{}, closeErr
	}
	a.mu.Lock()
	if current := a.sessions[req.SessionId]; current != nil && current.cancel != nil {
		a.mu.Unlock()
		return acp.LoadSessionResponse{}, acp.NewInvalidParams(map[string]any{"sessionId": req.SessionId, "reason": "prompt active"})
	}
	a.sessions[req.SessionId] = &sessionState{cwd: cwd, servers: servers, mode: "default"}
	a.mu.Unlock()
	return acp.LoadSessionResponse{Modes: sessionModes("default")}, nil
}

func (a *Agent) SetSessionMode(_ context.Context, req acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	if req.ModeId != "default" && req.ModeId != "read_only" {
		return acp.SetSessionModeResponse{}, acp.NewInvalidParams(map[string]any{"modeId": req.ModeId})
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.sessions[req.SessionId]
	if state == nil || state.cancel != nil {
		return acp.SetSessionModeResponse{}, acp.NewInvalidParams(map[string]any{"sessionId": req.SessionId})
	}
	state.mode = req.ModeId
	return acp.SetSessionModeResponse{}, nil
}

func (a *Agent) Prompt(ctx context.Context, req acp.PromptRequest) (acp.PromptResponse, error) {
	if a.connection() == nil {
		return acp.PromptResponse{}, fmt.Errorf("ACP connection is not ready")
	}
	prompt, err := promptText(req.Prompt)
	if err != nil {
		return acp.PromptResponse{}, err
	}
	a.mu.Lock()
	state := a.sessions[req.SessionId]
	if state == nil || state.cancel != nil {
		a.mu.Unlock()
		return acp.PromptResponse{}, acp.NewInvalidParams(map[string]any{"sessionId": req.SessionId})
	}
	turnCtx, cancel := context.WithCancel(ctx)
	state.cancel = cancel
	cwd, servers, readOnly := state.cwd, state.servers, state.mode == "read_only"
	a.mu.Unlock()
	defer func() {
		cancel()
		a.mu.Lock()
		state.cancel = nil
		a.mu.Unlock()
	}()

	turn := &turnUsage{}
	hook := &permissionHook{conn: a.connection(), sessionID: req.SessionId, cwd: cwd, readOnly: readOnly, usage: turn}
	runtime, err := a.open(turnCtx, cwd, readOnly, servers, hook)
	if err != nil {
		return acp.PromptResponse{}, err
	}
	textSent := false
	emit := func(ctx context.Context, event personal.Event) error {
		if event.Kind != string(agentcore.StreamEventTextDelta) || event.Text == "" {
			return nil
		}
		textSent = true
		return a.connection().SessionUpdate(ctx, acp.SessionNotification{SessionId: req.SessionId, Update: acp.UpdateAgentMessageText(event.Text)})
	}
	result, runErr := runtime.Runner.Run(turnCtx, personal.RunRequest{SessionID: string(req.SessionId), Prompt: prompt, Emit: emit})
	window := runtime.ContextWindow
	closeErr := runtime.close()
	if window <= 0 {
		window = appconfig.ReadContextWindow()
	}
	if turnCtx.Err() != nil || errors.Is(runErr, context.Canceled) {
		// A cancelled turn still consumed tokens; report them before returning.
		// The SDK cancels the request ctx on session/cancel, so detach from it
		// (bounded) or the notification could never be written.
		usageCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		usageErr := a.sendUsage(usageCtx, req.SessionId, state, turn, window)
		stop()
		return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, errors.Join(closeErr, usageErr)
	}
	if err := a.sendTurnUpdates(ctx, req.SessionId, state, turn, window, result); err != nil {
		return acp.PromptResponse{}, errors.Join(runErr, closeErr, err)
	}
	if errors.Is(runErr, ErrPermissionDenied) || errors.Is(runErr, ErrReadOnly) {
		return acp.PromptResponse{StopReason: acp.StopReasonRefusal}, closeErr
	}
	if err := errors.Join(runErr, closeErr); err != nil {
		return acp.PromptResponse{}, err
	}
	if !textSent && result.Output != "" {
		if err := a.connection().SessionUpdate(ctx, acp.SessionNotification{SessionId: req.SessionId, Update: acp.UpdateAgentMessageText(result.Output)}); err != nil {
			return acp.PromptResponse{}, err
		}
	}
	if result.Status == agentcore.RunYielded {
		return acp.PromptResponse{StopReason: acp.StopReasonMaxTurnRequests}, nil
	}
	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

// sendUsage reports session-cumulative usage after a turn (completed or cancelled).
func (a *Agent) sendUsage(ctx context.Context, id acp.SessionId, state *sessionState, turn *turnUsage, window int) error {
	total, used := state.recordUsage(a, turn)
	if err := a.connection().SessionUpdate(ctx, acp.SessionNotification{SessionId: id, Update: usageUpdate(total, used, window)}); err != nil {
		return fmt.Errorf("send usage update: %w", err)
	}
	return nil
}

// sendTurnUpdates reports usage and any complete task list in the model answer.
func (a *Agent) sendTurnUpdates(ctx context.Context, id acp.SessionId, state *sessionState, turn *turnUsage, window int, result personal.RunResult) error {
	if err := a.sendUsage(ctx, id, state, turn, window); err != nil {
		return err
	}
	if entries := planEntries(result.Output); len(entries) > 0 {
		if err := a.connection().SessionUpdate(ctx, acp.SessionNotification{SessionId: id, Update: acp.UpdatePlan(entries...)}); err != nil {
			return fmt.Errorf("send plan update: %w", err)
		}
	}
	return nil
}

// planEntries recognizes explicit Markdown checklists; prose is not treated as a plan.
func planEntries(output string) []acp.PlanEntry {
	var entries []acp.PlanEntry
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 6 || (line[:2] != "- " && line[:2] != "* ") || line[2] != '[' || line[4] != ']' || line[5] != ' ' {
			continue
		}
		status := acp.PlanEntryStatusPending
		switch line[3] {
		case 'x', 'X':
			status = acp.PlanEntryStatusCompleted
		case '>':
			status = acp.PlanEntryStatusInProgress
		case ' ':
		default:
			continue
		}
		if content := strings.TrimSpace(line[6:]); content != "" {
			entries = append(entries, acp.PlanEntry{Content: content, Status: status, Priority: acp.PlanEntryPriorityMedium})
		}
	}
	return entries
}

func (a *Agent) Cancel(_ context.Context, req acp.CancelNotification) error {
	a.mu.Lock()
	state := a.sessions[req.SessionId]
	var cancel context.CancelFunc
	if state != nil {
		cancel = state.cancel
	}
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func sessionModes(current acp.SessionModeId) *acp.SessionModeState {
	return &acp.SessionModeState{CurrentModeId: current, AvailableModes: []acp.SessionMode{
		{Id: "default", Name: "Default"}, {Id: "read_only", Name: "Read only"},
	}}
}

func sessionInputs(cwd string, roots []string, mcpServers []acp.McpServer) (string, []appconfig.MCPServerSettings, error) {
	if !filepath.IsAbs(cwd) || len(roots) != 0 {
		return "", nil, acp.NewInvalidParams(map[string]any{"cwd": cwd, "reason": "absolute cwd required; additional roots unsupported"})
	}
	servers, err := mcpSettings(mcpServers)
	if err != nil {
		return "", nil, err
	}
	return filepath.Clean(cwd), servers, nil
}

func promptText(blocks []acp.ContentBlock) (string, error) {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		switch {
		case block.Text != nil:
			parts = append(parts, block.Text.Text)
		case block.ResourceLink != nil:
			parts = append(parts, "Resource: "+block.ResourceLink.Uri)
		default:
			return "", acp.NewInvalidParams(map[string]any{"prompt": "unsupported content block"})
		}
	}
	return strings.Join(parts, "\n"), nil
}

func (*Agent) Authenticate(context.Context, acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, acp.NewMethodNotFound(acp.AgentMethodAuthenticate)
}
func (*Agent) Logout(context.Context, acp.LogoutRequest) (acp.LogoutResponse, error) {
	return acp.LogoutResponse{}, acp.NewMethodNotFound(acp.AgentMethodLogout)
}
func (*Agent) CloseSession(context.Context, acp.CloseSessionRequest) (acp.CloseSessionResponse, error) {
	return acp.CloseSessionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionClose)
}
func (*Agent) ListSessions(context.Context, acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	return acp.ListSessionsResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionList)
}
func (*Agent) ResumeSession(context.Context, acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	return acp.ResumeSessionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionResume)
}
func (*Agent) SetSessionConfigOption(context.Context, acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	return acp.SetSessionConfigOptionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionSetConfigOption)
}
