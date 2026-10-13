package native

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/inhere/suag/pkg/agentcore/tools"
	"golang.org/x/text/encoding/simplifiedchinese"

	"github.com/inhere/suag/pkg/agentcore/api"
)

const (
	// DefaultShellTimeout is used when the call gives no timeout.
	DefaultShellTimeout = 120 * time.Second
	// DefaultShellTimeoutMax caps the timeout a call may ask for
	// (tools.shell_timeout_max).
	DefaultShellTimeoutMax = 600 * time.Second
	// shellWaitDelay bounds how long Wait keeps reading pipes after the shell
	// exited or was killed: a background child holding stdout must not hang
	// the tool.
	shellWaitDelay = 2 * time.Second
)

// psPrefix makes PowerShell write UTF-8 (both its own output and what it
// pipes to native commands) so Chinese text is not mangled, turns off ANSI
// colors (PowerShell 7 only; they cost tokens and carry no information), and
// opens the script block psSuffix closes.
const psPrefix = "[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false);$OutputEncoding=[Console]::OutputEncoding;" +
	"if ($PSStyle) { $PSStyle.OutputRendering='PlainText' }\n& {\n"

// psSuffix pipes the command's output to Out-Default before exiting: implicit
// table formatting (Get-ChildItem, Select-Object, ...) waits about 300 ms to
// size its columns, and an exit inside that window dropped the whole table,
// so the model saw an empty result with exit code 0. It also makes the native
// command's exit code the process exit code; a failed cmdlet (with no native
// exit code) exits 1. -Command alone would report only whether the last
// statement succeeded.
const psSuffix = "\n$global:__ok=$? } | Out-Default\nif ($LASTEXITCODE) { exit $LASTEXITCODE }; if (-not $__ok) { exit 1 }; exit 0"

// shellSpec is the resolved shell the tool runs commands with.
type shellSpec struct {
	path       string // executable name or path
	powershell bool   // PowerShell argv, prefix and suffix
}

// shellBase is the lower-case executable name without directory or .exe; it
// splits on both separators so a Windows path is handled on any OS.
func shellBase(p string) string {
	return strings.TrimSuffix(strings.ToLower(p[strings.LastIndexAny(p, `/\`)+1:]), ".exe")
}

// name is the shell's display name for descriptions and prompts.
func (s shellSpec) name() string {
	base := shellBase(s.path)
	if s.powershell {
		if base == "pwsh" {
			return "PowerShell 7 (pwsh)"
		}
		return "Windows PowerShell"
	}
	return base
}

// argv builds the full command line for command.
func (s shellSpec) argv(command string) []string {
	if s.powershell {
		return []string{s.path, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", psPrefix + command + psSuffix}
	}
	return []string{s.path, "-c", command}
}

// resolveShell picks the shell: an explicit tools.shell wins (a command name
// or path, PowerShell detected by name); otherwise Windows uses pwsh, then
// powershell, and every other OS uses sh. bash is never auto-detected on
// Windows because the one on PATH may be WSL's.
func resolveShell(explicit string) shellSpec {
	isPS := func(p string) bool {
		base := shellBase(p)
		return base == "pwsh" || base == "powershell"
	}
	if explicit != "" {
		return shellSpec{path: explicit, powershell: isPS(explicit)}
	}
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("pwsh"); err == nil {
			return shellSpec{path: p, powershell: true}
		}
		return shellSpec{path: "powershell", powershell: true}
	}
	return shellSpec{path: "sh"}
}

// ShellName reports the shell shell_exec uses for the given tools.shell value
// (empty = OS default), for the system prompt.
func ShellName(explicit string) string { return resolveShell(explicit).name() }

// ShellExecTool runs a shell command inside the workspace directory. Every
// call is time-limited, the whole process tree is killed on timeout, and the
// output gathered so far is always returned with the exit code.
type ShellExecTool struct {
	workspace  string
	shell      shellSpec
	timeout    time.Duration // default per call
	maxTimeout time.Duration
	maxOut     int // raw result cap in bytes; 0 = DefaultRawOutputBytes
}

// NewShellExecTool returns a ShellExecTool scoped to workspace using the OS
// default shell. timeout is the default per call (0 = DefaultShellTimeout).
func NewShellExecTool(workspace string, timeout time.Duration) *ShellExecTool {
	if timeout <= 0 {
		timeout = DefaultShellTimeout
	}
	return &ShellExecTool{
		workspace:  workspace,
		shell:      resolveShell(""),
		timeout:    timeout,
		maxTimeout: DefaultShellTimeoutMax,
	}
}

func (t *ShellExecTool) Name() string { return "shell_exec" }
func (t *ShellExecTool) Description() string {
	syntax := "POSIX sh syntax"
	if t.shell.powershell {
		syntax = "PowerShell syntax (use ; not &&, $env:NAME for env vars)"
	}
	return fmt.Sprintf("Execute a command with %s in the workspace directory; write %s. "+
		"Returns stdout, stderr and the exit code; output gathered before a timeout is kept. "+
		"Default timeout %ds, max %ds. No interactive input: stdin is empty.",
		t.shell.name(), syntax, int(t.timeout.Seconds()), int(t.maxTimeout.Seconds()))
}

// ToolMeta marks the tool as an execute: it runs commands.
func (*ShellExecTool) ToolMeta() tools.Meta {
	return tools.Meta{Category: tools.CategoryExecute, ReadOnly: false}
}
func (t *ShellExecTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Command to execute",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": fmt.Sprintf("Timeout in seconds (default %d, max %d)", int(t.timeout.Seconds()), int(t.maxTimeout.Seconds())),
				},
			},
			"required": []any{"command"},
		},
	}
}

// callTimeout reads the optional timeout argument, clamped to maxTimeout.
func (t *ShellExecTool) callTimeout(args map[string]any) time.Duration {
	secs, ok := numberArg(args["timeout"])
	if !ok || secs <= 0 {
		return t.timeout
	}
	d := time.Duration(secs * float64(time.Second))
	if d > t.maxTimeout {
		return t.maxTimeout
	}
	return d
}

func (t *ShellExecTool) Execute(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	command, _ := call.Arguments["command"].(string)
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("shell_exec: command is required")
	}
	timeout := t.callTimeout(call.Arguments)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	argv := t.shell.argv(command)
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Dir = t.workspace
	cmd.Env = append(commandEnv(os.Environ()), "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "PAGER=cat")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr // stdin stays nil: the null device
	// tree is the OS process group / job object; Cancel kills all of it, and
	// WaitDelay forces Wait to return when a leftover child keeps a pipe open.
	tree := newProcessTree(cmd)
	cmd.Cancel = tree.kill
	cmd.WaitDelay = shellWaitDelay

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("shell_exec: start %s: %w", t.shell.name(), err)
	}
	tree.started()
	err := cmd.Wait()
	tree.close() // also ends background children left behind by the command

	// Colour codes carry nothing for the model (test libraries such as
	// gookit/assert colour their failure reports even without a terminal).
	var sb strings.Builder
	if out := ansiPattern.ReplaceAllString(decodeOutput(stdout.Bytes()), ""); out != "" {
		sb.WriteString(strings.TrimRight(out, "\r\n"))
	}
	if errOut := ansiPattern.ReplaceAllString(decodeOutput(stderr.Bytes()), ""); strings.TrimSpace(errOut) != "" {
		sb.WriteString("\n[stderr]\n")
		sb.WriteString(strings.TrimRight(errOut, "\r\n"))
	}
	output := TruncateHeadTail(strings.TrimLeft(sb.String(), "\n"), rawLimit(t.maxOut))

	exitCode := cmd.ProcessState.ExitCode()
	var status string
	switch {
	case runCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil:
		status = fmt.Sprintf("[timeout after %s: process tree killed; output above is partial]", timeout)
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case errors.Is(err, exec.ErrWaitDelay):
		status = fmt.Sprintf("[exit code: %d; a background process kept the output open and was killed]", exitCode)
	case err != nil && exitCode < 0:
		status = fmt.Sprintf("[error: %v]", err)
	default:
		status = fmt.Sprintf("[exit code: %d]", exitCode)
	}
	content := status
	if output != "" {
		content = output + "\n" + status
	}
	return &api.ToolResult{
		CallID:   call.CallID,
		Name:     t.Name(),
		Content:  content,
		Metadata: map[string]any{"exit_code": exitCode},
	}, nil
}

// decodeOutput returns b as text. Non-UTF-8 output (a native Windows program
// writing in the ANSI code page) is decoded as GBK as a best-effort fallback.
func decodeOutput(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	if decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(b); err == nil {
		return string(decoded)
	}
	return strings.ToValidUTF8(string(b), "�")
}

// numberArg reads a JSON number argument that models sometimes send as a
// string.
func numberArg(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, !math.IsNaN(n)
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		var f float64
		if _, err := fmt.Sscan(strings.TrimSpace(n), &f); err == nil {
			return f, true
		}
	}
	return 0, false
}

// privateEnv are suag's own settings. Commands run for the user's project do
// not inherit them: a project's code or tests that read SUAG_HOME (suag's
// own tests, for one) would otherwise act on the agent's configuration.
var privateEnv = []string{"SUAG_HOME", "SUAG_CONFIG", "SUAG_MODEL"}

// commandEnv returns env without privateEnv (names compared without case:
// Windows environment names are case-insensitive).
func commandEnv(env []string) []string {
	out := make([]string, 0, len(env))
next:
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		for _, p := range privateEnv {
			if strings.EqualFold(name, p) {
				continue next
			}
		}
		out = append(out, kv)
	}
	return out
}
