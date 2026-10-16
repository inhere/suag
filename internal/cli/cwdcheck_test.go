package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal"
	"github.com/inhere/suga/internal/personal/store"
)

// newGuardSession records a session with a working directory in the store the
// CLI's guard reads (the one under SUGA_HOME).
func newGuardSession(t *testing.T, id, cwd string) {
	t.Helper()
	repo, err := openCoreStore()
	assert.Require(t, assert.NoErr(t, err))
	defer repo.Close()
	err = repo.CreateSession(context.Background(), store.Session{ID: id, Title: "guard", Cwd: cwd})
	assert.Require(t, assert.NoErr(t, err))
}

// TestGuardResumeCwd: the check in front of a continue. Every case reports what
// the caller has to do and, on a failure or a question, what the person saw.
func TestGuardResumeCwd(t *testing.T) {
	t.Setenv("SUGA_HOME", t.TempDir())
	ctx := context.Background()
	// The directories exist, so "go back to the session's directory" is offered.
	dirA, dirB := t.TempDir(), t.TempDir()
	gone := filepath.Join(t.TempDir(), "gone")

	guard := func(t *testing.T, opts personalOptions, current string, interactive bool, input string) (resumeCwdAction, string, error) {
		t.Helper()
		var stderr bytes.Buffer
		action, err := guardResumeCwd(ctx, opts, current, interactive, strings.NewReader(input), &stderr)
		return action, stderr.String(), err
	}

	t.Run("the same directory is not asked about", func(t *testing.T) {
		newGuardSession(t, "s-same", dirA)
		action, out, err := guard(t, personalOptions{ResumeID: "s-same"}, dirA, true, "")
		assert.NoErr(t, err)
		assert.Eq(t, cwdAgreed, action)
		assert.Eq(t, "", out)
	})

	t.Run("the same directory in another spelling is not asked about", func(t *testing.T) {
		newGuardSession(t, "s-dot", filepath.Join(dirA, ".", "sub", ".."))
		action, out, err := guard(t, personalOptions{ResumeID: "s-dot"}, dirA, true, "")
		assert.NoErr(t, err)
		assert.Eq(t, cwdAgreed, action)
		assert.Eq(t, "", out)
	})

	t.Run("a session without a recorded directory is left to the runner", func(t *testing.T) {
		newGuardSession(t, "s-old", "")
		action, out, err := guard(t, personalOptions{ResumeID: "s-old"}, dirB, true, "")
		assert.NoErr(t, err)
		assert.Eq(t, cwdAgreed, action)
		assert.Eq(t, "", out)
	})

	t.Run("an unknown session is left to the not-found error", func(t *testing.T) {
		action, _, err := guard(t, personalOptions{ResumeID: "missing"}, dirB, true, "")
		assert.NoErr(t, err)
		assert.Eq(t, cwdAgreed, action)
	})

	t.Run("a handoff starts a new session in this directory", func(t *testing.T) {
		newGuardSession(t, "s-source", dirA)
		action, _, err := guard(t, personalOptions{FromID: "s-source", SessionID: "s-source"}, dirB, true, "")
		assert.NoErr(t, err)
		assert.Eq(t, cwdAgreed, action)
	})

	t.Run("without a terminal the continue fails and names both ways on", func(t *testing.T) {
		newGuardSession(t, "s-run", dirA)
		action, _, err := guard(t, personalOptions{ResumeID: "s-run"}, dirB, false, "")
		assert.Eq(t, cwdAgreed, action)
		assert.Require(t, assert.Err(t, err))
		msg := err.Error()
		assert.Eq(t, true, strings.Contains(msg, "s-run"))
		assert.Eq(t, true, strings.Contains(msg, dirA))
		assert.Eq(t, true, strings.Contains(msg, dirB))
		assert.Eq(t, true, strings.Contains(msg, "cd "+dirA))
		assert.Eq(t, true, strings.Contains(msg, "--allow-cwd-change"))
		assert.Eq(t, 1, ExitCode(err))
	})

	t.Run("a recorded directory that is gone is named as such", func(t *testing.T) {
		newGuardSession(t, "s-gone", gone)
		_, _, err := guard(t, personalOptions{ResumeID: "s-gone"}, dirB, false, "")
		assert.Require(t, assert.Err(t, err))
		assert.Eq(t, true, strings.Contains(err.Error(), gone+"（该目录已不存在）"))
	})

	t.Run("--allow-cwd-change continues here without asking", func(t *testing.T) {
		newGuardSession(t, "s-allow", dirA)
		action, out, err := guard(t, personalOptions{ResumeID: "s-allow", AllowCwdChange: true}, dirB, true, "")
		assert.NoErr(t, err)
		assert.Eq(t, cwdUseCurrent, action)
		assert.Eq(t, "", out)
	})

	t.Run("a terminal is asked the three choices", func(t *testing.T) {
		newGuardSession(t, "s-ask", dirA)
		action, out, err := guard(t, personalOptions{ResumeID: "s-ask"}, dirB, true, "")
		assert.NoErr(t, err)
		assert.Eq(t, cwdCancelled, action) // end of input leaves chat
		assert.Eq(t, true, strings.Contains(out, "会话 s-ask 创建于目录 "+dirA))
		assert.Eq(t, true, strings.Contains(out, "当前工作目录是 "+dirB))
		assert.Eq(t, true, strings.Contains(out, "  1) 切到 "+dirA+" 续接"))
		assert.Eq(t, true, strings.Contains(out, "  2) 在 "+dirB+" 继续"))
		assert.Eq(t, true, strings.Contains(out, "  3) 取消"))
	})

	t.Run("choice 3 cancels", func(t *testing.T) {
		newGuardSession(t, "s-cancel", dirA)
		action, _, err := guard(t, personalOptions{ResumeID: "s-cancel"}, dirB, true, "3\n")
		assert.NoErr(t, err)
		assert.Eq(t, cwdCancelled, action)
	})

	t.Run("choice 2 continues here", func(t *testing.T) {
		newGuardSession(t, "s-here", dirA)
		action, _, err := guard(t, personalOptions{ResumeID: "s-here"}, dirB, true, "2\n")
		assert.NoErr(t, err)
		assert.Eq(t, cwdUseCurrent, action)
	})

	t.Run("choice 1 moves the process to the session's directory", func(t *testing.T) {
		newGuardSession(t, "s-back", dirA)
		t.Chdir(dirB)
		action, _, err := guard(t, personalOptions{ResumeID: "s-back"}, dirB, true, "1\n")
		assert.NoErr(t, err)
		assert.Eq(t, cwdUseSession, action)
		wd, err := os.Getwd()
		assert.NoErr(t, err)
		assert.Eq(t, true, personal.SameDir(dirA, wd))
	})

	t.Run("garbage is asked again and 1 is unavailable for a gone directory", func(t *testing.T) {
		newGuardSession(t, "s-gone-ask", gone)
		action, out, err := guard(t, personalOptions{ResumeID: "s-gone-ask"}, dirB, true, "x\n1\n3\n")
		assert.NoErr(t, err)
		assert.Eq(t, cwdCancelled, action)
		assert.Eq(t, true, strings.Contains(out, "  1) 切到原目录续接（"+gone+" 已不存在，不可选）"))
		assert.Eq(t, true, strings.Contains(out, "请输入 1、2 或 3。"))
		assert.Eq(t, true, strings.Contains(out, "原目录 "+gone+" 已不存在，只能选 2 或 3。"))
	})
}

// TestResumeCwdChangeEndToEnd drives the whole CLI: a session created in one
// directory is continued in another, refused by default and accepted with
// --allow-cwd-change, which moves the recorded directory and records it.
func TestResumeCwdChangeEndToEnd(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	t.Setenv("SUGA_HOME", t.TempDir())
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(server.Close)
	setTestProvider(t, server.URL)
	assert.NoErr(t, gkconfig.Set("model", "openai/gpt-4o"))

	wsA, wsB := t.TempDir(), t.TempDir()
	ctx := context.Background()
	t.Chdir(wsA)
	assert.NoErr(t, RunPersonalCLI(ctx, "run", []string{"--prompt", "hi", "--session-id", "cwd-e2e", "--read-only"},
		strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}))
	assert.Eq(t, int32(1), requests.Load())
	assert.Eq(t, true, personal.SameDir(wsA, storedCwd(t, "cwd-e2e")))

	t.Chdir(wsB)
	t.Run("run --resume refuses and starts no turn", func(t *testing.T) {
		var stderr bytes.Buffer
		err := RunPersonalCLI(ctx, "run", []string{"--prompt", "again", "--resume", "cwd-e2e", "--read-only"},
			strings.NewReader(""), &bytes.Buffer{}, &stderr)
		assert.Require(t, assert.Err(t, err))
		assert.Eq(t, 1, ExitCode(err))
		msg := err.Error()
		assert.Eq(t, true, strings.Contains(msg, wsA) && strings.Contains(msg, wsB))
		assert.Eq(t, true, strings.Contains(msg, "--allow-cwd-change"))
		assert.Eq(t, int32(1), requests.Load()) // the model was not asked
		assert.Eq(t, true, personal.SameDir(wsA, storedCwd(t, "cwd-e2e")))
		assert.Eq(t, 0, len(sessionEvents(t, "cwd-e2e")))
	})

	t.Run("chat without a terminal refuses as well", func(t *testing.T) {
		var stderr bytes.Buffer
		err := RunPersonalCLI(ctx, "chat", []string{"--resume", "cwd-e2e"},
			strings.NewReader(":quit\n"), &bytes.Buffer{}, &stderr)
		assert.Require(t, assert.Err(t, err))
		assert.Eq(t, 1, ExitCode(err))
		assert.Eq(t, true, strings.Contains(err.Error(), wsA) && strings.Contains(err.Error(), wsB))
		assert.Eq(t, int32(1), requests.Load())
	})

	t.Run("--allow-cwd-change continues here and moves the session", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := RunPersonalCLI(ctx, "run", []string{"--prompt", "here", "--resume", "cwd-e2e", "--read-only", "--allow-cwd-change"},
			strings.NewReader(""), &stdout, &stderr)
		assert.NoErr(t, err)
		assert.Eq(t, int32(2), requests.Load())
		assert.Eq(t, true, strings.Contains(stdout.String(), "ok"))
		assert.Eq(t, true, personal.SameDir(wsB, storedCwd(t, "cwd-e2e")))

		events := sessionEvents(t, "cwd-e2e")
		assert.Require(t, assert.Eq(t, 1, len(events)))
		assert.Eq(t, "session_cwd_changed", events[0].Name)
		assert.Eq(t, true, personal.SameDir(wsA, fmt.Sprint(events[0].Attrs["from"])))
		assert.Eq(t, true, personal.SameDir(wsB, fmt.Sprint(events[0].Attrs["to"])))
	})

	t.Run("the moved directory is the one a later resume is compared with", func(t *testing.T) {
		t.Chdir(wsA)
		err := RunPersonalCLI(ctx, "run", []string{"--prompt", "back", "--resume", "cwd-e2e", "--read-only"},
			strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
		assert.Require(t, assert.Err(t, err))
		assert.Eq(t, true, strings.Contains(err.Error(), wsA) && strings.Contains(err.Error(), wsB))
		assert.Eq(t, int32(2), requests.Load())
	})
}

// storedCwd reads the working directory a session records.
func storedCwd(t *testing.T, sessionID string) string {
	t.Helper()
	repo, err := openCoreStore()
	assert.Require(t, assert.NoErr(t, err))
	defer repo.Close()
	model, err := repo.GetSession(context.Background(), sessionID)
	assert.Require(t, assert.NoErr(t, err))
	return model.Cwd
}

// sessionEvents returns the session's lifecycle events (kind "session").
func sessionEvents(t *testing.T, sessionID string) []store.Event {
	t.Helper()
	repo, err := openCoreStore()
	assert.Require(t, assert.NoErr(t, err))
	defer repo.Close()
	events, err := repo.ListEvents(context.Background(), store.EventFilter{SessionID: sessionID, Kinds: []string{store.EventSession}})
	assert.Require(t, assert.NoErr(t, err))
	return events
}
