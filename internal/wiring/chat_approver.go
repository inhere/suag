package wiring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	gkconfig "github.com/gookit/config/v2"
	"github.com/inhere/suga/internal/adddir"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/redact"
	"github.com/inhere/suga/pkg/agentcore/tools"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

// chatApprover is the chat tools.Approver: MCP tools keep the cliMCPGate rules
// (never asked), read-only tools (tools.MetaOf) run, and every other tool
// (file_write, shell_exec and any tool that declares nothing) is confirmed by
// the person (tools.approval: ask) or run (tools.approval: auto). A file call
// that reaches outside the workspace and the session's directories is asked
// about separately (chat.ask_outside_dirs), read-only tools included.
type chatApprover struct {
	gate      *cliMCPGate
	auto      bool
	asker     tools.Asker
	workspace string
	// liveDirs is the session's additional-directory set: a file_remove prompt for
	// a path under a :rw directory is planned with the same rules as the workspace.
	liveDirs *native.AddDirs
	// readRoots are the configured read-only roots (native.RootPaths): a path under
	// one is already reachable and needs no authorization.
	readRoots []string
	// readOnly marks a --read-only session: it has no write tool at all, so only a
	// read-only directory can be authorized.
	readOnly bool
	// askOutside turns the outside-directory question on (chat.ask_outside_dirs,
	// on by default); addDirs is the session's added-directory handle it works
	// through (nil: the question stays off).
	askOutside bool
	addDirs    *SessionAddDirs

	// mu serializes questions and guards always: the tools answered "a"
	// (always) run without asking for the rest of this process.
	mu     sync.Mutex
	always map[string]bool
}

// chatApproverOptions are the inputs of the chat approver, read when the runtime
// is assembled (the session does not exist yet).
type chatApproverOptions struct {
	gate       *cliMCPGate
	auto       bool
	asker      tools.Asker
	workspace  string
	dirs       *native.AddDirs
	readRoots  []string
	readOnly   bool
	askOutside bool
}

func newChatApprover(opts chatApproverOptions) *chatApprover {
	return &chatApprover{gate: opts.gate, auto: opts.auto, asker: opts.asker, workspace: opts.workspace,
		liveDirs: opts.dirs, readRoots: opts.readRoots, readOnly: opts.readOnly, askOutside: opts.askOutside,
		always: map[string]bool{}}
}

// SetAddDirs wires the session's added-directory handle: the outside-directory
// question authorizes a directory through it, the same handle ApplyAddDirs
// publishes the session's directories with. nil keeps the question off.
func (a *chatApprover) SetAddDirs(dirs *SessionAddDirs) { a.addDirs = dirs }

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
	if tools.IsMCP(tool) {
		return a.gate.Decide(ctx, call, tool)
	}
	// A file call that reaches outside the workspace and the session's
	// directories is asked about first, read-only tools included: what such a
	// call needs is a new directory, not the permission for one call, so the
	// question stands apart from the per-call approvals below (a granted write
	// still gets its own confirmation there).
	if err, stop := a.askOutsideDir(ctx, call); stop {
		return err
	}
	name := call.Name
	// AgentState tools only change the agent's own state; the tool enforces
	// its own write rules per run mode, so no question is asked.
	if meta := tools.MetaOf(tool); meta.ReadOnly || meta.AgentState || a.auto {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.always[name] {
		tools.ReportApproval(ctx, tools.ApprovedAlways, 0)
		return nil
	}
	asked := time.Now()
	answer, err := a.asker.Ask(ctx, a.question(call))
	if errors.Is(err, tools.ErrCannotAsk) {
		return tools.Denied(fmt.Sprintf("无法确认执行 %s（标准输入不是终端），已拒绝；如需在 chat 中自动放行，请设置 tools.approval: auto", name))
	}
	if err != nil {
		return err // not a refusal: the approval layer aborts the turn
	}
	wait := time.Since(asked)
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		tools.ReportApproval(ctx, tools.ApprovedByUser, wait)
		return nil
	case "a", "always":
		a.always[name] = true
		tools.ReportApproval(ctx, tools.ApprovedByUser, wait)
		return nil
	}
	tools.ReportApproval(ctx, tools.AskedAndDenied, wait)
	return tools.Denied("用户拒绝执行 " + name)
}

// askOutsideDir asks the person to authorize the directory a file tool call
// wants to reach outside the workspace and the session's added directories
// (chat.ask_outside_dirs), before the call runs. The question's subject is the
// directory the tool would really resolve (native.OutsideOf), never a path the
// model described in words, so a call cannot talk its way into another
// directory.
//
// A granted directory is recorded and published at once (source ask), so this
// call and the ones after it reach the directory; the call then goes on through
// the ordinary rules, so a write still gets its own confirmation. A refusal is a
// denied call, and the tool keeps refusing by itself. stop is true when the call
// was decided here; it is false when this call needs no question (another tool,
// a reachable path, the question turned off, no session handle, or no terminal
// to ask on - a piped chat refuses as ever) and when a question was answered
// with a grant.
//
// Nothing ran before the question, so the call itself reaches the directory in
// this step. The one exception is a read that the runtime had already run ahead
// of the turn (tools.parallel_reads, two or more prefetchable calls in one
// reply): its result stays the pre-grant refusal, and the model simply calls it
// again with the directory now in place.
func (a *chatApprover) askOutsideDir(ctx context.Context, call agentcore.ToolCall) (err error, stop bool) {
	if !a.askOutside || a.addDirs == nil {
		return nil, false
	}
	pending, ok := native.OutsideOf(a.workspace, a.readRoots, a.liveDirs, call.Name, call.Arguments)
	if !ok {
		return nil, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	askedAt := time.Now()
	answer, askErr := a.asker.Ask(ctx, a.outsideQuestion(call, pending))
	if errors.Is(askErr, tools.ErrCannotAsk) {
		// No terminal: the tool refuses the path by itself, and a message here
		// would only hide that reason.
		return nil, false
	}
	if askErr != nil {
		return askErr, true
	}
	wait := time.Since(askedAt)
	writable, refuse := outsideAnswer(answer)
	if refuse {
		tools.ReportApproval(ctx, tools.AskedAndDenied, wait)
		return tools.Denied("用户拒绝授权访问 " + pending.Path), true
	}
	if _, err := a.addDirs.add(ctx, adddir.Dir{Path: pending.Path, Writable: writable}, adddir.SourceAsk); err != nil {
		tools.ReportApproval(ctx, tools.AskedAndDenied, wait)
		return tools.Denied("授权访问 " + pending.Path + " 失败：" + err.Error()), true
	}
	tools.ReportApproval(ctx, tools.ApprovedByUser, wait)
	// The call runs now: nothing was refused before the question, so this very
	// step reaches the directory (its result follows the answer).
	return nil, false
}

// outsideAnswer reads the answer to an outside-directory question: [r] a
// read-only directory (the default for anything that is not [w]), [w] a
// read-write one. refuse is true for "n", an empty answer and anything else.
func outsideAnswer(answer string) (writable, refuse bool) {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "r", "ro", "read", "只读":
		return false, false
	case "w", "rw", "write", "读写":
		return true, false
	}
	return false, true
}

// outsideQuestion is the question of one outside-directory authorization: the
// tool that asked, the path it gave and the canonical directory to authorize.
// A --read-only session offers the read-only answer only: it has no write tool
// to use a read-write directory with.
func (a *chatApprover) outsideQuestion(call agentcore.ToolCall, pending native.OutsideDir) tools.Question {
	details := []string{"工具：" + call.Name, "要访问：" + pending.Target}
	if pending.Path != pending.Target {
		details = append(details, "目录："+pending.Path)
	}
	prompt := fmt.Sprintf("授权访问 %s？[r] 只读 [w] 读写 [n] 拒绝：", pending.Path)
	if a.readOnly {
		prompt = fmt.Sprintf("授权访问 %s？[r] 只读 [n] 拒绝（本会话为 --read-only）：", pending.Path)
	}
	for i, detail := range details {
		details[i] = displaySafe(redact.Secrets(detail))
	}
	return tools.Question{Tool: call.Name, Details: details, Prompt: prompt}
}

// question shows the full key arguments of the call: redacted, never truncated.
func (a *chatApprover) question(call agentcore.ToolCall) tools.Question {
	args := call.Arguments
	var details []string
	switch call.Name {
	case "shell_exec":
		details = []string{"命令：" + argText(args["command"])}
	case "file_remove":
		details = []string{"路径：" + a.fullPath(argText(args["path"]))}
		plan, err := native.PlanRemoveWithDirs(a.workspace, a.liveDirs.WritePaths(), args)
		switch {
		case err != nil:
			details = append(details, "检查："+err.Error())
		case plan.Purge:
			details = append(details, "被忽略的可再生内容：直接永久删除，不进回收区")
		case plan.Dir:
			details = append(details, fmt.Sprintf("目录：%d 个文件，%d 字节，移入回收区保留 7 天", plan.Files, plan.Bytes))
		default:
			details = append(details, fmt.Sprintf("文件：%d 字节，移入回收区保留 7 天", plan.Bytes))
		}
		if err == nil && len(plan.PurgeDirs) > 0 {
			details = append(details, fmt.Sprintf("直接永久删除 %d 个被忽略的目录：%s", len(plan.PurgeDirs), native.NotedDirs(plan.PurgeDirs)))
		}
	case "file_write":
		details = []string{
			"路径：" + a.fullPath(argText(args["path"])),
			fmt.Sprintf("内容：%d 字节", len(argText(args["content"]))),
		}
	case "file_edit":
		details = []string{"路径：" + a.fullPath(argText(args["path"]))}
		edits, err := native.EditList(args)
		if err != nil {
			raw, _ := json.Marshal(args["edits"])
			details = append(details, "参数："+string(raw))
		}
		for i, edit := range edits {
			label := ""
			if len(edits) > 1 {
				label = fmt.Sprintf("第 %d 处", i+1)
			}
			details = append(details,
				label+"原文：\n"+previewLines(argText(edit["old_string"])),
				label+"改为：\n"+previewLines(argText(edit["new_string"])))
			if all, _ := edit["replace_all"].(bool); all {
				details = append(details, label+"替换全部匹配处")
			}
		}
	case "web_search":
		details = []string{"查询：" + argText(args["query"])}
	default:
		raw, _ := json.Marshal(args)
		details = []string{"参数：" + string(raw)}
	}
	for i, detail := range details {
		details[i] = displaySafe(redact.Secrets(detail))
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
