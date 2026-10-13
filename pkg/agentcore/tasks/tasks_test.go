package tasks

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/pkg/agentcore/llm"
)

// The test binary doubles as the task command: TASKS_HELPER selects what it
// does (see TestMain).
func TestMain(m *testing.M) {
	switch os.Getenv("TASKS_HELPER") {
	case "":
		os.Exit(m.Run())
	case "lines":
		n, _ := strconv.Atoi(os.Getenv("TASKS_LINES"))
		for i := 1; i <= n; i++ {
			fmt.Printf("line %d token=sk-secret\n", i)
		}
		fmt.Fprint(os.Stderr, "last without newline")
		code, _ := strconv.Atoi(os.Getenv("TASKS_EXIT"))
		os.Exit(code)
	case "sleep":
		fmt.Println("ready")
		time.Sleep(time.Minute)
	case "parent":
		// A child that keeps stdout open: only a tree kill ends it at once.
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "TASKS_HELPER=sleep")
		child.Stdout = os.Stdout
		_ = child.Start()
		fmt.Println("parent ready")
		time.Sleep(time.Minute)
	}
	os.Exit(0)
}

func helper(mode string, env ...string) *exec.Cmd {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(append(os.Environ(), "TASKS_HELPER="+mode), env...)
	return cmd
}

func newTestManager(t *testing.T, limits Limits, onExit func(Info)) (*Manager, context.Context) {
	t.Helper()
	dir := t.TempDir()
	m := NewManager(Config{Limits: limits, OnExit: onExit, Output: func(sid, id string) (string, string, error) {
		return filepath.Join(dir, sid+"-"+id+".txt"), "out:" + sid + ":" + id, nil
	}})
	t.Cleanup(m.Close)
	return m, llm.WithSessionID(context.Background(), "s1")
}

// waitFor polls cond for up to 10s.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatal("condition not met in time")
}

func TestManager(t *testing.T) {
	clean := func(b []byte) string { return strings.ReplaceAll(string(b), "sk-secret", "[REDACTED]") }

	t.Run("runs, writes cleaned lines, waits", func(t *testing.T) {
		m, ctx := newTestManager(t, Limits{}, nil)
		info, err := m.Start(ctx, Spec{Kind: "shell", Summary: "lines", Cmd: helper("lines", "TASKS_LINES=3", "TASKS_EXIT=3"), Clean: clean})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "t1", info.ID)
		assert.Eq(t, "out:s1:t1", info.Ref)
		info, done, err := m.Wait(ctx, "s1", "t1", 10*time.Second)
		assert.Require(t, assert.NoErr(t, err))
		assert.True(t, done)
		assert.Eq(t, Exited, info.Status)
		assert.Eq(t, 3, info.ExitCode)
		assert.Eq(t, 4, info.Lines) // the last one without a newline too
		assert.Eq(t, "last without newline", info.Tail[len(info.Tail)-1])
		data, _ := os.ReadFile(info.Path)
		assert.StrContains(t, string(data), "line 2 token=[REDACTED]\n")
		assert.NotContains(t, string(data), "sk-secret")
		assert.Empty(t, m.TakeNotices("s1")) // Wait saw the end
		lines, total, more, err := ReadLines(info.Path, 2, 2)
		assert.NoErr(t, err)
		assert.Eq(t, []string{"line 2 token=[REDACTED]", "line 3 token=[REDACTED]"}, lines)
		assert.Eq(t, 4, total)
		assert.True(t, more)
		hits, matched, err := GrepLines(info.Path, `line [13]`, 10)
		assert.NoErr(t, err)
		assert.Eq(t, 2, matched)
		assert.Eq(t, "3: line 3 token=[REDACTED]", hits[1])
	})
	t.Run("an unseen end leaves one notice and calls OnExit", func(t *testing.T) {
		var exits atomic.Int32
		m, ctx := newTestManager(t, Limits{}, func(Info) { exits.Add(1) })
		_, err := m.Start(ctx, Spec{Kind: "shell", Cmd: helper("lines", "TASKS_LINES=1")})
		assert.Require(t, assert.NoErr(t, err))
		waitFor(t, func() bool { return m.HasNotices("s1") })
		notices := m.TakeNotices("s1")
		assert.Require(t, assert.Len(t, notices, 1))
		assert.Eq(t, Exited, notices[0].Status)
		assert.Empty(t, m.TakeNotices("s1"))
		waitFor(t, func() bool { return exits.Load() == 1 })
	})
	t.Run("kill ends the whole tree at once", func(t *testing.T) {
		m, ctx := newTestManager(t, Limits{}, nil)
		info, err := m.Start(ctx, Spec{Kind: "shell", Cmd: helper("parent")})
		assert.Require(t, assert.NoErr(t, err))
		waitFor(t, func() bool { i, _ := m.Get("s1", info.ID); return i.Lines >= 2 }) // parent and child are up
		start := time.Now()
		info, err = m.Kill("s1", info.ID)
		assert.NoErr(t, err)
		assert.Eq(t, Killed, info.Status)
		// A child left alive would hold the pipe until WaitDelay (2s).
		assert.True(t, time.Since(start) < 1500*time.Millisecond, time.Since(start))
		assert.Empty(t, m.TakeNotices("s1"))
	})
	t.Run("limits: running count, duration, output size", func(t *testing.T) {
		m, ctx := newTestManager(t, Limits{MaxRunning: 1, MaxDuration: 300 * time.Millisecond, MaxOutputBytes: 100}, nil)
		_, err := m.Start(ctx, Spec{Kind: "shell", Cmd: helper("sleep")})
		assert.Require(t, assert.NoErr(t, err))
		_, err = m.Start(ctx, Spec{Kind: "shell", Cmd: helper("sleep")})
		assert.ErrMsgContains(t, err, "already running")
		info, done, err := m.Wait(ctx, "s1", "t1", 10*time.Second)
		assert.NoErr(t, err)
		assert.True(t, done)
		assert.Eq(t, TimedOut, info.Status)

		info, err = m.Start(ctx, Spec{Kind: "shell", Cmd: helper("lines", "TASKS_LINES=50")})
		assert.Require(t, assert.NoErr(t, err))
		info, _, _ = m.Wait(ctx, "s1", info.ID, 10*time.Second)
		assert.True(t, info.Truncated)
		assert.True(t, info.Bytes <= 100)
		data, _ := os.ReadFile(info.Path)
		assert.StrContains(t, string(data), "[output cut at 100 bytes; 51 lines in all]")
	})
	t.Run("sessions are separate; close kills everything", func(t *testing.T) {
		m, ctx := newTestManager(t, Limits{}, nil)
		info, err := m.Start(ctx, Spec{Kind: "shell", Cmd: helper("sleep")})
		assert.Require(t, assert.NoErr(t, err))
		_, err = m.Get("s2", info.ID)
		assert.ErrIs(t, err, ErrUnknownTask)
		_, err = m.Start(context.Background(), Spec{Kind: "shell", Cmd: helper("sleep")})
		assert.ErrMsgContains(t, err, "need a session")
		m.Close()
		got, _ := m.Get("s1", info.ID)
		assert.Eq(t, Killed, got.Status)
		_, err = m.Start(ctx, Spec{Kind: "shell", Cmd: helper("sleep")})
		assert.ErrMsgContains(t, err, "shut down")
	})
}
