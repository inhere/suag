package wiring

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	gkconfig "github.com/gookit/config/v2"

	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

const debugPromptNonceEnv = "SUGA_DEBUG_PROMPT_NONCE"

// detectInterpreters probes PATH for script interpreters (tools.interpreters).
// It is shared by the environment section (environmentPrompt) and the native
// tool catalog (shell_exec's description), and is a variable so tests can run
// without depending on the machine's PATH.
var detectInterpreters = func() []native.Interpreter {
	return native.DetectInterpreters(gkconfig.Get("tools.interpreters"))
}

// staticPrompt is a system prompt built once per process. It holds no time or
// per-turn data, so every turn of the process sends the same bytes and the
// provider's prefix cache keeps hitting.
type staticPrompt string

func (p staticPrompt) BuildSystemPrompt(context.Context, *agentcore.RunState) (string, error) {
	return string(p), nil
}

// basePrompt is the system prompt: identity and working conventions only
// (design D1). It holds nothing that depends on the workspace, the read/write
// mode, rules, memory or skills, so two sessions of the same configuration send
// byte-identical system prompts and the provider prefix cache can reuse them
// across sessions and workspaces. The environment (environmentPrompt) and the
// skills listing are given to the runner as sources and go into the segment
// context; the tool list is not repeated: tool schemas already carry it.
func basePrompt() string {
	var b strings.Builder
	// SUGA_DEBUG_PROMPT_NONCE is a hidden debug switch for the eval's
	// --probe-prefix control group: a non-empty value is put first so the
	// whole prompt prefix differs per run and cannot hit a provider cache.
	// It is never set in normal use.
	if nonce := os.Getenv(debugPromptNonceEnv); nonce != "" {
		b.WriteString("nonce: " + nonce + "\n")
	}
	b.WriteString("You are suga, a personal coding agent working in the user's workspace through tools.\n\n")
	b.WriteString("# Working conventions\n")
	b.WriteString("- Use the file tools to read, list and search files rather than shell commands.\n")
	// Step-count review (docs/research/2026-10-11-suag-vs-omp-loop-analysis.md,
	// suag-jz0l): exploration took one tool per step, and most extra steps came
	// after the tests already passed.
	b.WriteString("- Put independent reads, searches and commands in the same response so they run together (several files in one file_read, a search plus a listing, ...).\n")
	// R6 write-to-green review: suga made one edit per step where omp sent
	// all fixes for a test run at once; file_edit's edits went unused.
	b.WriteString("- Make independent changes together: several replacements in one file in one file_edit call (edits), and edits to different files in the same response, e.g. every fix a failed test run calls for.\n")
	// Dependency APIs per language (suga-ej2w): the Go line alone sent the Java
	// session to the shell to find jars and run javap by hand.
	b.WriteString("- To learn a dependency's API, use one command instead of reading its files one by one: `go doc <package>` (or a grep for the exported names) for Go; for Java, `javap -public -cp <jar> <fully.qualified.Class>` with the jar under ~/.m2/repository/<group path>/<artifact>/<version>/ (`mvn -o dependency:sources` fetches the sources).\n")
	b.WriteString("- Read a file whole when it fits in one file_read; use offset/limit for a part you need, such as the lines a failure points to.\n")
	b.WriteString("- Read a file before changing it; prefer small exact edits over rewriting whole files. A successful edit or write needs no re-read: the tool result is the verification.\n")
	b.WriteString("- Verify from narrow to broad: run the test closest to your change, then the package or suite once. When the checks pass, stop verifying: do not re-run them (-count, -v, coverage) or re-read your files to double-check; check again only after something failed or changed.\n")
	b.WriteString("- Shell commands cannot be interactive and have a timeout; pass a larger timeout for long builds or tests.\n")
	// Java-session review (local/analysis/2026-10-14-java-sessions.md, suga-5e99):
	// 8 of 9 mvn calls piped the output through Select-Object/Select-String or
	// used -q, so the model filtered out the very lines the minimizer keeps and
	// had to re-run the build to see them.
	b.WriteString("- Build and test output (go test, mvn, gradle, npm test, pytest, ...) is minimized into a summary plus the failure details; the untidied output stays under the call's ref. Do not filter it yourself (no `| tail`, `| Select-String`, `| Select-Object -Last`) and do not pass `-q` to mvn or gradle: both hide the summary and the failures you need.\n")
	b.WriteString("- Delete files and directories with file_remove (it keeps them in a trash), not with shell rm/del/Remove-Item.\n")
	b.WriteString("- When a tool is refused or not enabled, do not retry it; explain what is needed instead.\n")
	// Kept from the step-count experiment: a mid-task memory write disappeared
	// with it, while the other step-count conventions made no difference
	// (docs/research/2026-10-11-suag-vs-omp-loop-analysis.md).
	b.WriteString("- Keep to the task: do not create or update tracker issues, plans or memory entries while doing it unless the user asks for it or corrects how you work.\n")
	b.WriteString("- Change only what the task asks for. Problems you notice outside that scope go into a \"Found, not touched\" section of your report instead of being fixed on the side.\n")
	b.WriteString("- If injected rules, memory or docs contradict the actual code or environment, say so in your report (what, where, why it looks stale) instead of silently working around it.\n")
	b.WriteString("- Answer in the user's language.\n")
	return strings.TrimRight(b.String(), "\n")
}

// environmentPrompt is the environment section of the segment context: OS,
// workspace, shell (writable), read/write mode and how to enable writing. It
// is read for every new segment and compared at each turn start.
func environmentPrompt(opts Options) string {
	var b strings.Builder
	b.WriteString("# Environment\n")
	fmt.Fprintf(&b, "- OS: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&b, "- Workspace (file tools are confined to it): %s\n", opts.Workspace)
	if roots, _ := readRoots(); len(roots) > 0 {
		// Dependency sources: read them with the file tools instead of shell
		// detours (docs/research/2026-10-11-suag-vs-omp-loop-analysis.md).
		b.WriteString("- Read-only roots outside the workspace (file_read, file_list, file_grep and file_find accept absolute paths under them):\n")
		for _, r := range roots {
			fmt.Fprintf(&b, "  - %s: %s\n", r.Label, r.Path)
		}
	}
	if dirs := opts.sessionDirs.Snapshot(); len(dirs) > 0 {
		// The session's own additional directories (--add-dir): named so the
		// model uses the file tools there instead of a shell detour.
		b.WriteString("- Additional directories of this session (--add-dir; the file tools accept absolute paths under them):\n")
		for _, d := range dirs {
			mode := "read-only"
			if d.Writable {
				mode = "read-write — file_write, file_edit and file_remove may change files there, like in the workspace"
			}
			fmt.Fprintf(&b, "  - %s: %s\n", d.Path, mode)
		}
	}
	if opts.AllowWrite {
		fmt.Fprintf(&b, "- Shell for shell_exec: %s\n", native.ShellName(gkconfig.String("tools.shell")))
		// Interpreters found on PATH (tools.interpreters): with one at hand the
		// model should write a script file instead of quoting complex text on a
		// shell line (suga-bkt8). Nothing is written when none was found — the
		// model stays with the shell — and none is written in read-only mode,
		// where shell_exec is off. Only the segment context carries this line, so
		// a newly installed interpreter does not invalidate the prompt prefix
		// cache; the probe itself runs once per process (DetectInterpreters
		// caches per setting).
		if hint := native.InterpreterHint(detectInterpreters()); hint != "" {
			fmt.Fprintf(&b, "- %s\n", hint)
		}
		b.WriteString("- Mode: writable — you may edit files and run commands; the user may be asked to confirm.\n")
	} else {
		b.WriteString("- Mode: read-only — tools that write files or run commands are off. ")
		b.WriteString("If the task needs them, say so and tell the user how to enable them: ")
		b.WriteString(writeToolHint(opts))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
