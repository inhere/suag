package acpbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/inhere/suag/internal/host/session"
)

type CommandConfig struct {
	Command []string
	Env     map[string]string
}

type CommandSpec struct {
	Args    []string
	Workdir string
	Env     map[string]string
}

type CommandOutput struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type CommandExecutor func(ctx context.Context, spec CommandSpec) (CommandOutput, error)

type LocalCommandBridgeConfig struct {
	Commands map[string]CommandConfig
	Executor CommandExecutor
	Now      func() time.Time
}

type LocalCommandBridge struct {
	commands map[string]CommandConfig
	executor CommandExecutor
	now      func() time.Time
}

type localCommandResult struct {
	State           string   `json:"state"`
	Summary         string   `json:"summary"`
	Error           string   `json:"error"`
	Artifacts       []string `json:"artifacts"`
	RemoteAgentName string   `json:"remote_agent_name"`
	RemoteSessionID string   `json:"remote_session_id"`
	RemoteTaskID    string   `json:"remote_task_id"`
}

func NewLocalCommandBridge(cfg LocalCommandBridgeConfig) *LocalCommandBridge {
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	executor := cfg.Executor
	if executor == nil {
		executor = execLocalCommand
	}

	return &LocalCommandBridge{
		commands: copyCommandConfigs(cfg.Commands),
		executor: executor,
		now:      nowFn,
	}
}

func (b *LocalCommandBridge) Dispatch(ctx context.Context, req DispatchRequest) (DispatchResult, error) {
	if b == nil {
		return DispatchResult{}, fmt.Errorf("local command bridge is required")
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

	output, err := b.executor(ctx, spec)
	if err != nil {
		return DispatchResult{}, fmt.Errorf("execute local adapter %q: %w", req.Adapter, err)
	}

	return b.parseOutput(req, output), nil
}

func (b *LocalCommandBridge) parseOutput(req DispatchRequest, output CommandOutput) DispatchResult {
	trimmedStdout := strings.TrimSpace(output.Stdout)
	trimmedStderr := strings.TrimSpace(output.Stderr)
	now := b.now().UTC()

	if trimmedStdout != "" {
		var decoded localCommandResult
		if err := json.Unmarshal([]byte(trimmedStdout), &decoded); err == nil {
			state := StateCompleted
			if parsed := parseDispatchState(decoded.State); parsed != "" {
				state = parsed
			}
			summary := strings.TrimSpace(decoded.Summary)
			if summary == "" {
				summary = trimmedStdout
			}
			return DispatchResult{
				State:           state,
				Summary:         summary,
				Error:           strings.TrimSpace(decoded.Error),
				Artifacts:       append([]string(nil), decoded.Artifacts...),
				RemoteAgentName: firstNonEmpty(decoded.RemoteAgentName, req.Adapter),
				RemoteSessionID: firstNonEmpty(decoded.RemoteSessionID, session.NewSessionID()),
				RemoteTaskID:    firstNonEmpty(decoded.RemoteTaskID, session.NewExternalTaskID()),
				LastHeartbeatAt: now,
			}
		}
	}

	summary := trimmedStdout
	if summary == "" {
		summary = trimmedStderr
	}

	return DispatchResult{
		State:           StateCompleted,
		Summary:         summary,
		RemoteAgentName: req.Adapter,
		RemoteSessionID: session.NewSessionID(),
		RemoteTaskID:    session.NewExternalTaskID(),
		LastHeartbeatAt: now,
	}
}

func execLocalCommand(ctx context.Context, spec CommandSpec) (CommandOutput, error) {
	if len(spec.Args) == 0 {
		return CommandOutput{}, fmt.Errorf("command args are required")
	}

	cmd := exec.CommandContext(ctx, spec.Args[0], spec.Args[1:]...)
	if spec.Workdir != "" {
		cmd.Dir = spec.Workdir
	}
	cmd.Env = mergeCommandEnv(spec.Env)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := CommandOutput{
		Stdout: strings.TrimSpace(stdout.String()),
		Stderr: strings.TrimSpace(stderr.String()),
	}

	if cmd.ProcessState != nil {
		output.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		return output, err
	}
	return output, nil
}

func copyCommandConfigs(src map[string]CommandConfig) map[string]CommandConfig {
	if len(src) == 0 {
		return map[string]CommandConfig{}
	}

	dst := make(map[string]CommandConfig, len(src))
	for key, cfg := range src {
		copied := CommandConfig{
			Command: append([]string(nil), cfg.Command...),
		}
		if len(cfg.Env) > 0 {
			copied.Env = make(map[string]string, len(cfg.Env))
			for envKey, envValue := range cfg.Env {
				copied.Env[envKey] = envValue
			}
		}
		dst[key] = copied
	}
	return dst
}

func expandCommandArgs(args []string, req DispatchRequest) []string {
	expanded := make([]string, 0, len(args))
	replacer := strings.NewReplacer(
		"{session_id}", req.SessionID,
		"{turn_id}", req.TurnID,
		"{run_id}", req.RunID,
		"{agent_id}", req.AgentID,
		"{adapter}", req.Adapter,
		"{instruction}", req.Instruction,
		"{workspace}", req.Workspace,
	)
	for _, arg := range args {
		expanded = append(expanded, replacer.Replace(arg))
	}
	return expanded
}

func buildCommandEnv(extra map[string]string, req DispatchRequest) map[string]string {
	env := map[string]string{
		"AXON_SESSION_ID":  req.SessionID,
		"AXON_TURN_ID":     req.TurnID,
		"AXON_RUN_ID":      req.RunID,
		"AXON_AGENT_ID":    req.AgentID,
		"AXON_ADAPTER":     req.Adapter,
		"AXON_WORKSPACE":   req.Workspace,
		"AXON_INSTRUCTION": req.Instruction,
	}
	for key, value := range extra {
		env[key] = value
	}
	return env
}

func mergeCommandEnv(extra map[string]string) []string {
	base := os.Environ()
	if len(extra) == 0 {
		return base
	}

	out := append([]string(nil), base...)
	for key, value := range extra {
		out = append(out, key+"="+value)
	}
	return out
}

func parseDispatchState(raw string) DispatchState {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(StateRunning):
		return StateRunning
	case string(StateCompleted):
		return StateCompleted
	case string(StateFailed):
		return StateFailed
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
