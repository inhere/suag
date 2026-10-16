package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	appconfig "github.com/inhere/suga/internal/config"
	"github.com/inhere/suga/internal/personal"
	"github.com/inhere/suga/internal/personal/store"
)

// resumeCwdAction is what the working-directory check of a session this
// invocation continues decided the caller should do.
type resumeCwdAction int

const (
	// cwdAgreed: the session and this invocation work in the same directory, or
	// there was nothing to compare (a new session, an old one without a recorded
	// directory, or a handoff).
	cwdAgreed resumeCwdAction = iota
	// cwdUseCurrent: continue in this directory; sessions.cwd is moved here by
	// the caller (chat's second choice, or --allow-cwd-change).
	cwdUseCurrent
	// cwdUseSession: this process was moved into the session's directory
	// (chat's first choice), so everything below runs there.
	cwdUseSession
	// cwdCancelled: the person chose to leave (chat's third choice): nothing is
	// opened and nothing is written.
	cwdCancelled
)

// guardResumeCwd compares the working directory recorded on the session this
// invocation continues (sessions.cwd) with current, the directory it would work
// in. It runs before the runtime is assembled, so no tool call can see the
// wrong directory and no turn is started for a continue that is refused.
//
// On a mismatch a terminal chat asks what to do (interactive), everything else
// fails: run and a chat without a terminal (a pipe, a job runner) exit with an
// error naming both directories and the two ways on, `cd <directory>` or
// --allow-cwd-change. That flag, like chat's second choice, continues in the
// current directory and reports cwdUseCurrent. An unknown session and a session
// without a recorded directory are left to the normal flow (the runner fills an
// empty cwd in on the next turn).
func guardResumeCwd(ctx context.Context, opts personalOptions, current string,
	interactive bool, stdin io.Reader, stderr io.Writer) (resumeCwdAction, error) {
	if opts.FromID != "" {
		// A handoff starts a new session; its directory is the one this process
		// works in, and the source session is only read.
		return cwdAgreed, nil
	}
	sessionID := opts.ResumeID
	if sessionID == "" {
		sessionID = opts.SessionID
	}
	if sessionID == "" {
		return cwdAgreed, nil
	}
	recorded, err := recordedSessionCwd(ctx, sessionID)
	if err != nil {
		return cwdAgreed, err
	}
	if strings.TrimSpace(recorded) == "" || personal.SameDir(recorded, current) {
		return cwdAgreed, nil
	}
	if opts.AllowCwdChange {
		return cwdUseCurrent, nil
	}
	if !interactive {
		return cwdAgreed, cwdMismatchError(sessionID, recorded, current)
	}
	return askResumeCwd(sessionID, recorded, current, stdin, stderr)
}

// recordedSessionCwd reads the working directory stored on a session. A missing
// session is not an error here: opening it reports it (exit code 4 with
// "session not found") and a --session-id that does not exist yet is simply a
// new session.
func recordedSessionCwd(ctx context.Context, sessionID string) (string, error) {
	repo, err := openCoreStore()
	if err != nil {
		return "", err
	}
	defer repo.Close()
	model, err := repo.GetSession(ctx, sessionID)
	if errors.Is(err, store.ErrSessionNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return model.Cwd, nil
}

// cwdMismatchError is the failure of a continue in another directory without a
// terminal to ask at: it names both directories and both ways to go on.
func cwdMismatchError(sessionID, recorded, current string) error {
	dir := recorded
	if !isDir(recorded) {
		dir += "（该目录已不存在）"
	}
	return fmt.Errorf("会话 %s 的工作目录是 %s，当前工作目录是 %s；"+
		"请先 `cd %s` 再续接，或加 --allow-cwd-change 明确在当前目录继续", sessionID, dir, current, recorded)
}

// askResumeCwd asks at the terminal what to do with a session created in
// another directory: continue in the session's directory (this process moves
// there before the runtime is assembled, so the workspace, the shell and the
// session's short name all follow), continue here (the recorded directory is
// moved), or cancel without touching anything. The three answers are the only
// accepted input; EOF (Ctrl-D) cancels, like the third one.
func askResumeCwd(sessionID, recorded, current string, stdin io.Reader, stderr io.Writer) (resumeCwdAction, error) {
	// The directory is checked once: the question itself says when going back is
	// no longer possible.
	back := isDir(recorded)
	fmt.Fprintf(stderr, "会话 %s 创建于目录 %s\n当前工作目录是 %s\n", sessionID, recorded, current)
	if back {
		fmt.Fprintf(stderr, "  1) 切到 %s 续接\n", recorded)
	} else {
		fmt.Fprintf(stderr, "  1) 切到原目录续接（%s 已不存在，不可选）\n", recorded)
	}
	fmt.Fprintf(stderr, "  2) 在 %s 继续（把会话目录改为这里）\n", current)
	fmt.Fprintf(stderr, "  3) 取消（直接退出，不改动任何数据）\n")
	for {
		fmt.Fprint(stderr, "请选择 [1/2/3]: ")
		answer, err := readChoice(stdin)
		switch strings.TrimSpace(answer) {
		case "1":
			if !back {
				fmt.Fprintf(stderr, "原目录 %s 已不存在，只能选 2 或 3。\n", recorded)
				continue
			}
			if cerr := os.Chdir(recorded); cerr != nil {
				return cwdAgreed, fmt.Errorf("切换到会话目录 %s 失败：%w（可改选 2 或 3）", recorded, cerr)
			}
			return cwdUseSession, nil
		case "2":
			return cwdUseCurrent, nil
		case "3":
			return cwdCancelled, nil
		}
		if err != nil {
			// End of input (Ctrl-D): leave, like the third answer.
			return cwdCancelled, nil
		}
		fmt.Fprintln(stderr, "请输入 1、2 或 3。")
	}
}

// readChoice reads one line of the terminal by the byte, so nothing typed after
// it is swallowed by a buffer chat would then never see.
func readChoice(stdin io.Reader) (string, error) {
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := stdin.Read(buf)
		if n > 0 {
			switch buf[0] {
			case '\r', '\n':
				return b.String(), nil
			case 0x7f, '\b': // a typo may be corrected before Enter
				s := b.String()
				if s != "" {
					b.Reset()
					b.WriteString(s[:len(s)-1])
				}
			default:
				b.WriteByte(buf[0])
			}
		}
		if err != nil {
			return b.String(), err
		}
	}
}

// isDir reports whether path is an existing directory (the session's recorded
// directory may be gone: another machine, a rename, a deleted checkout).
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// effectiveWorkspace is the directory this invocation works in, resolved the
// way wiring.OpenCLI resolves the tool workspace: the configured `workspace`,
// else the directory the process was started in.
func effectiveWorkspace() (string, error) {
	abs, err := filepath.Abs(appconfig.ResolveWorkspacePath())
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	return abs, nil
}

// isTerminalInput reports whether stdin is a terminal; chat's own input source
// makes the same distinction (a pipe is not asked a question).
func isTerminalInput(stdin io.Reader) bool {
	file, ok := stdin.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}
