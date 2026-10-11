package wiring

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	gkconfig "github.com/gookit/config/v2"

	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/tools/native"
)

const debugPromptNonceEnv = "SUAG_DEBUG_PROMPT_NONCE"

// staticPrompt is a system prompt built once per process. It holds no time or
// per-turn data, so every turn of the process sends the same bytes and the
// provider's prefix cache keeps hitting.
type staticPrompt string

func (p staticPrompt) BuildSystemPrompt(context.Context, *agentcore.RunState) (string, error) {
	return string(p), nil
}

// basePrompt is the environment part of the system prompt: OS and shell,
// workspace, read-only or writable (and how to enable writing), working
// conventions (including scope discipline and reporting stale rules), and the skills listing (empty until skills are wired). The
// tool list is not repeated here: tool schemas already carry it. Project
// rules (AGENTS.md) and the append prompt follow it in the runner.
func basePrompt(opts Options, skills string) string {
	var b strings.Builder
	// SUAG_DEBUG_PROMPT_NONCE is a hidden debug switch for the eval's
	// --probe-prefix control group: a non-empty value is put first so the
	// whole prompt prefix differs per run and cannot hit a provider cache.
	// It is never set in normal use.
	if nonce := os.Getenv(debugPromptNonceEnv); nonce != "" {
		b.WriteString("nonce: " + nonce + "\n")
	}
	// Order matters for prompt caching: the text that is the same for every
	// session (identity, conventions, skills) comes first so providers can
	// reuse the cached prefix across sessions and workspaces; the
	// per-workspace environment (path, mode) comes last.
	b.WriteString("You are suag, a personal coding agent working in the user's workspace through tools.\n\n")
	b.WriteString("# Working conventions\n")
	b.WriteString("- Use the file tools to read, list and search files rather than shell commands.\n")
	b.WriteString("- Read large files in windows with file_read offset/limit instead of all at once.\n")
	b.WriteString("- Read a file before changing it; prefer small exact edits over rewriting whole files.\n")
	b.WriteString("- Shell commands cannot be interactive and have a timeout; pass a larger timeout for long builds or tests.\n")
	b.WriteString("- When a tool is refused or not enabled, do not retry it; explain what is needed instead.\n")
	b.WriteString("- Change only what the task asks for. Problems you notice outside that scope go into a \"Found, not touched\" section of your report instead of being fixed on the side.\n")
	b.WriteString("- If injected rules, memory or docs contradict the actual code or environment, say so in your report (what, where, why it looks stale) instead of silently working around it.\n")
	b.WriteString("- Answer in the user's language.\n")
	if skills != "" {
		b.WriteString("\n")
		b.WriteString(strings.TrimRight(skills, "\n"))
		b.WriteString("\n")
	}
	b.WriteString("\n# Environment\n")
	fmt.Fprintf(&b, "- OS: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&b, "- Workspace (file tools are confined to it): %s\n", opts.Workspace)
	if opts.AllowWrite {
		fmt.Fprintf(&b, "- Shell for shell_exec: %s\n", native.ShellName(gkconfig.String("tools.shell")))
		b.WriteString("- Mode: writable — you may edit files and run commands; the user may be asked to confirm.\n")
	} else {
		b.WriteString("- Mode: read-only — tools that write files or run commands are off. ")
		b.WriteString("If the task needs them, say so and tell the user how to enable them: ")
		b.WriteString(writeToolHint(opts))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
