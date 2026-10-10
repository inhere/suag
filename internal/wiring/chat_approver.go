package wiring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	gkconfig "github.com/gookit/config/v2"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/pkg/agentcore"
)

// chatReadOnlyTools never need a confirmation in chat. Every other non-MCP tool
// (file_write, shell_exec and any tool added later) is asked about.
var chatReadOnlyTools = map[string]bool{
	"file_read": true, "file_list": true, "update_plan": true, "tool_search": true,
	"web_search": true, "file_grep": true, "file_find": true, "web_fetch": true, "skill": true,
}

// chatApprover is the chat tools.Approver: MCP tools keep the cliMCPGate rules
// (never asked), read-only tools run, and the other tools are confirmed by the
// person (tools.approval: ask) or run (tools.approval: auto).
type chatApprover struct {
	gate      *cliMCPGate
	auto      bool
	asker     tools.Asker
	workspace string

	// mu serializes questions and guards always: the tools answered "a"
	// (always) run without asking for the rest of this process.
	mu     sync.Mutex
	always map[string]bool
}

func newChatApprover(gate *cliMCPGate, auto bool, asker tools.Asker, workspace string) *chatApprover {
	return &chatApprover{gate: gate, auto: auto, asker: asker, workspace: workspace, always: map[string]bool{}}
}

// toolsApprovalAuto reads tools.approval: "ask" (default) or "auto". Any other
// value is a startup error (fail closed), never guessed.
func toolsApprovalAuto() (bool, error) {
	switch value := gkconfig.Get("tools.approval").(type) {
	case nil:
		return false, nil
	case string:
		switch value {
		case "ask":
			return false, nil
		case "auto":
			return true, nil
		}
	}
	return false, fmt.Errorf("tools.approval must be \"ask\" or \"auto\", got %v", gkconfig.Get("tools.approval"))
}

func (a *chatApprover) Decide(ctx context.Context, call agentcore.ToolCall, tool agentcore.Tool) error {
	if tools.KindOf(tool) == tools.KindMCP {
		return a.gate.Decide(ctx, call, tool)
	}
	name := call.Name
	if chatReadOnlyTools[name] || a.auto {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.always[name] {
		return nil
	}
	answer, err := a.asker.Ask(ctx, a.question(call))
	if errors.Is(err, tools.ErrCannotAsk) {
		return tools.Denied(fmt.Sprintf("无法确认执行 %s（标准输入不是终端），已拒绝；如需在 chat 中自动放行，请设置 tools.approval: auto", name))
	}
	if err != nil {
		return err // not a refusal: the approval layer aborts the turn
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	case "a", "always":
		a.always[name] = true
		return nil
	}
	return tools.Denied("用户拒绝执行 " + name)
}

// question shows the full key arguments of the call: redacted, never truncated.
func (a *chatApprover) question(call agentcore.ToolCall) tools.Question {
	args := call.Arguments
	var details []string
	switch call.Name {
	case "shell_exec":
		details = []string{"命令：" + argText(args["command"])}
	case "file_write":
		details = []string{
			"路径：" + a.fullPath(argText(args["path"])),
			fmt.Sprintf("内容：%d 字节", len(argText(args["content"]))),
		}
	case "file_edit":
		details = []string{
			"路径：" + a.fullPath(argText(args["path"])),
			"原文：\n" + previewLines(argText(args["old_string"])),
			"改为：\n" + previewLines(argText(args["new_string"])),
		}
		if all, _ := args["replace_all"].(bool); all {
			details = append(details, "替换全部匹配处")
		}
	case "web_search":
		details = []string{"查询：" + argText(args["query"])}
	default:
		raw, _ := json.Marshal(args)
		details = []string{"参数：" + string(raw)}
	}
	for i, detail := range details {
		details[i] = displaySafe(personal.RedactSecretValues(detail))
	}
	return tools.Question{
		Tool:    call.Name,
		Details: details,
		Prompt:  fmt.Sprintf("允许执行 %s？[y]是 [N]否 [a]本会话总是允许该工具：", call.Name),
	}
}

// editPreviewLines bounds each side of a file_edit preview.
const editPreviewLines = 20

// previewLines returns text cut to editPreviewLines lines, noting the total.
func previewLines(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= editPreviewLines {
		return text
	}
	return strings.Join(lines[:editPreviewLines], "\n") + fmt.Sprintf("\n…（共 %d 行）", len(lines))
}

// fullPath resolves a workspace-relative path for display.
func (a *chatApprover) fullPath(path string) string {
	if path == "" || filepath.IsAbs(path) || a.workspace == "" {
		return path
	}
	return filepath.Join(a.workspace, path)
}

func argText(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(raw)
}

// displaySafe escapes control and bidi characters (except newline and tab) so
// the model cannot hide part of a command from the person reading it, e.g.
// with "\r" or an ANSI erase sequence.
func displaySafe(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case unicode.IsControl(r), r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
