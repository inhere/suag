package acpbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/inhere/suag/internal/host/session"
)

type ACPStdioSession interface {
	WriteLine(ctx context.Context, line []byte) error
	ReadLine(ctx context.Context) ([]byte, error)
	Close() error
}

type ACPStdioSessionStarter func(ctx context.Context, spec CommandSpec) (ACPStdioSession, error)

type ACPStdioBridgeConfig struct {
	Commands       map[string]CommandConfig
	SessionStarter ACPStdioSessionStarter
	Now            func() time.Time
}

type ACPStdioBridge struct {
	commands map[string]CommandConfig
	starter  ACPStdioSessionStarter
	now      func() time.Time
}

type acpRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type acpRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *acpRPCError    `json:"error,omitempty"`
}

type acpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type acpPromptResult struct {
	StopReason    string `json:"stopReason"`
	UserMessageID string `json:"userMessageId"`
}

type acpSessionNewResult struct {
	SessionID string `json:"sessionId"`
}

type acpSessionUpdateEnvelope struct {
	SessionID string         `json:"sessionId"`
	Update    map[string]any `json:"update"`
}

type acpInitializeParams struct {
	ProtocolVersion    int               `json:"protocolVersion"`
	ClientCapabilities map[string]any    `json:"clientCapabilities"`
	ClientInfo         map[string]string `json:"clientInfo"`
}

func NewACPStdioBridge(cfg ACPStdioBridgeConfig) *ACPStdioBridge {
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	starter := cfg.SessionStarter
	if starter == nil {
		starter = startACPStdioSession
	}

	return &ACPStdioBridge{
		commands: copyCommandConfigs(cfg.Commands),
		starter:  starter,
		now:      nowFn,
	}
}

func (b *ACPStdioBridge) Dispatch(ctx context.Context, req DispatchRequest) (DispatchResult, error) {
	if b == nil {
		return DispatchResult{}, fmt.Errorf("acp stdio bridge is required")
	}
	if strings.TrimSpace(req.Adapter) == "" {
		return DispatchResult{}, fmt.Errorf("adapter is required")
	}
	if strings.TrimSpace(req.Instruction) == "" {
		return DispatchResult{}, fmt.Errorf("instruction is required")
	}

	cmdCfg, ok := b.commands[strings.TrimSpace(req.Adapter)]
	if !ok {
		return DispatchResult{}, fmt.Errorf("adapter %q is not configured", req.Adapter)
	}
	if len(cmdCfg.Command) == 0 {
		return DispatchResult{}, fmt.Errorf("adapter %q command is empty", req.Adapter)
	}

	spec := CommandSpec{
		Args:    expandCommandArgs(cmdCfg.Command, req),
		Workdir: strings.TrimSpace(req.Workspace),
		Env:     buildCommandEnv(cmdCfg.Env, req),
	}

	sess, err := b.starter(ctx, spec)
	if err != nil {
		return DispatchResult{}, fmt.Errorf("start acp stdio session: %w", err)
	}
	defer sess.Close()

	if err := b.sendInitialize(ctx, sess); err != nil {
		return DispatchResult{}, err
	}
	remoteSessionID, err := b.sendSessionNew(ctx, sess, req.Workspace)
	if err != nil {
		return DispatchResult{}, err
	}
	summary, remoteTaskID, err := b.sendPrompt(ctx, sess, remoteSessionID, req.Instruction)
	if err != nil {
		return DispatchResult{}, err
	}

	return DispatchResult{
		State:           StateCompleted,
		Summary:         summary,
		RemoteAgentName: req.Adapter,
		RemoteSessionID: remoteSessionID,
		RemoteTaskID:    firstNonEmpty(remoteTaskID, session.NewExternalTaskID()),
		LastHeartbeatAt: b.now().UTC(),
	}, nil
}

func (b *ACPStdioBridge) sendInitialize(ctx context.Context, sess ACPStdioSession) error {
	req := acpRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: acpInitializeParams{
			ProtocolVersion: 1,
			ClientCapabilities: map[string]any{
				"fs": map[string]bool{
					"readTextFile":  false,
					"writeTextFile": false,
				},
				"terminal": false,
			},
			ClientInfo: map[string]string{
				"name":    "suag",
				"version": "v0",
			},
		},
	}
	return b.sendRequestExpectResult(ctx, sess, req, nil)
}

func (b *ACPStdioBridge) sendSessionNew(ctx context.Context, sess ACPStdioSession, workdir string) (string, error) {
	req := acpRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "session/new",
		Params: map[string]any{
			"cwd":        workdir,
			"mcpServers": []any{},
		},
	}

	var result acpSessionNewResult
	if err := b.sendRequestExpectResult(ctx, sess, req, &result); err != nil {
		return "", err
	}
	if strings.TrimSpace(result.SessionID) == "" {
		return "", fmt.Errorf("session/new returned empty session id")
	}
	return result.SessionID, nil
}

func (b *ACPStdioBridge) sendPrompt(ctx context.Context, sess ACPStdioSession, remoteSessionID, instruction string) (string, string, error) {
	req := acpRPCRequest{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "session/prompt",
		Params: map[string]any{
			"sessionId": remoteSessionID,
			"prompt": []map[string]any{
				{
					"type": "text",
					"text": instruction,
				},
			},
		},
	}

	if err := writeACPLine(ctx, sess, req); err != nil {
		return "", "", fmt.Errorf("write session/prompt request: %w", err)
	}

	var summaryBuilder strings.Builder
	for {
		msg, err := readACPResponse(ctx, sess)
		if err != nil {
			return "", "", fmt.Errorf("read acp prompt response: %w", err)
		}

		if msg.Method == "session/update" {
			appendACPMessageChunk(&summaryBuilder, msg.Params)
			continue
		}
		if msg.ID != 3 {
			continue
		}
		if msg.Error != nil {
			return "", "", fmt.Errorf("session/prompt error: %s", strings.TrimSpace(msg.Error.Message))
		}

		var result acpPromptResult
		if len(msg.Result) > 0 {
			if err := json.Unmarshal(msg.Result, &result); err != nil {
				return "", "", fmt.Errorf("decode session/prompt result: %w", err)
			}
		}
		return strings.TrimSpace(summaryBuilder.String()), strings.TrimSpace(result.UserMessageID), nil
	}
}

func (b *ACPStdioBridge) sendRequestExpectResult(ctx context.Context, sess ACPStdioSession, req acpRPCRequest, out any) error {
	if err := writeACPLine(ctx, sess, req); err != nil {
		return fmt.Errorf("write %s request: %w", req.Method, err)
	}

	for {
		msg, err := readACPResponse(ctx, sess)
		if err != nil {
			return fmt.Errorf("read %s response: %w", req.Method, err)
		}
		if msg.Method == "session/update" {
			continue
		}
		if msg.ID != req.ID {
			continue
		}
		if msg.Error != nil {
			return fmt.Errorf("%s error: %s", req.Method, strings.TrimSpace(msg.Error.Message))
		}
		if out != nil && len(msg.Result) > 0 {
			if err := json.Unmarshal(msg.Result, out); err != nil {
				return fmt.Errorf("decode %s result: %w", req.Method, err)
			}
		}
		return nil
	}
}

func writeACPLine(ctx context.Context, sess ACPStdioSession, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal acp payload: %w", err)
	}
	return sess.WriteLine(ctx, append(data, '\n'))
}

func readACPResponse(ctx context.Context, sess ACPStdioSession) (acpRPCResponse, error) {
	line, err := sess.ReadLine(ctx)
	if err != nil {
		return acpRPCResponse{}, err
	}

	var msg acpRPCResponse
	if err := json.Unmarshal(line, &msg); err != nil {
		return acpRPCResponse{}, fmt.Errorf("decode acp message: %w", err)
	}
	return msg, nil
}

func appendACPMessageChunk(builder *strings.Builder, rawParams json.RawMessage) {
	var envelope acpSessionUpdateEnvelope
	if err := json.Unmarshal(rawParams, &envelope); err != nil {
		return
	}

	if strings.TrimSpace(stringValue(envelope.Update["sessionUpdate"])) != "agent_message_chunk" {
		return
	}
	content, ok := envelope.Update["content"].(map[string]any)
	if !ok {
		return
	}
	if strings.TrimSpace(stringValue(content["type"])) != "text" {
		return
	}
	builder.WriteString(stringValue(content["text"]))
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if raw, ok := value.(string); ok {
		return raw
	}
	return ""
}

type processACPStdioSession struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
}

func startACPStdioSession(ctx context.Context, spec CommandSpec) (ACPStdioSession, error) {
	if len(spec.Args) == 0 {
		return nil, fmt.Errorf("command args are required")
	}

	cmd := exec.CommandContext(ctx, spec.Args[0], spec.Args[1:]...)
	if spec.Workdir != "" {
		cmd.Dir = spec.Workdir
	}
	cmd.Env = mergeCommandEnv(spec.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if _, err := cmd.StderrPipe(); err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start process: %w", err)
	}

	return &processACPStdioSession{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
	}, nil
}

func (s *processACPStdioSession) WriteLine(_ context.Context, line []byte) error {
	_, err := s.stdin.Write(line)
	return err
}

func (s *processACPStdioSession) ReadLine(_ context.Context) ([]byte, error) {
	return s.stdout.ReadBytes('\n')
}

func (s *processACPStdioSession) Close() error {
	if s == nil {
		return nil
	}
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	if s.cmd.ProcessState != nil {
		return nil
	}
	_ = s.cmd.Process.Kill()
	_, _ = s.cmd.Process.Wait()
	return nil
}
