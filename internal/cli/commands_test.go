package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/internal/personal/process"
	"github.com/inhere/suag/internal/personal/store"
)

func TestCLIRegistersPersonalCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"--help"})
	assert.NoErr(t, err)
	for _, name := range []string{"run", "chat", "acp"} {
		assert.Eq(t, true, strings.Contains(stdout.String(), name))
	}
}

func TestCLIRegistersProcessCommandsAndSendExitCodes(t *testing.T) {
	home := shortCLIHome(t)
	t.Setenv("SUAG_HOME", home)
	root := filepath.Join(home, "run")
	delivered := make(chan string, 1)
	server, err := process.Start(root, "session-cli-123", "CLI test", t.TempDir(), func(text, from string) (bool, error) {
		delivered <- from + ":" + text
		return false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	var stdout, stderr bytes.Buffer
	err = newCLI(context.Background(), strings.NewReader("stdin delivery"), &stdout, &stderr).RunWithArgs([]string{"send", "--session", "session-cli"})
	assert.NoErr(t, err)
	assert.Eq(t, "cli:stdin delivery", <-delivered)
	assert.Eq(t, "delivered\n", stderr.String())

	// --source replaces the default "cli" source label.
	stderr.Reset()
	err = newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"send", "--session", "session-cli", "--source", "web-ui", "--text", "hi"})
	assert.NoErr(t, err)
	assert.Eq(t, "web-ui:hi", <-delivered)
	assert.Eq(t, "delivered\n", stderr.String())

	stdout.Reset()
	stderr.Reset()
	err = newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"ps", "--json"})
	assert.NoErr(t, err)
	var records []process.Record
	assert.NoErr(t, json.Unmarshal(stdout.Bytes(), &records))
	assert.Eq(t, 1, len(records))
	assert.Eq(t, "session-cli-123", records[0].SessionID)

	err = newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"send", "--session", "missing-session", "--text", "hello"})
	var exitCoder interface{ ExitCode() int }
	if !errors.As(err, &exitCoder) || exitCoder.ExitCode() != 3 {
		t.Fatalf("offline session should return exit code 3, got %v", err)
	}
	if got := ExitCode(err); got != 3 {
		t.Fatalf("CLI main mapping returned %d for offline session", got)
	}
	if got := ExitCode(fmt.Errorf("wrapped: %w", personal.SessionNotFoundError{ID: "x"})); got != 4 {
		t.Fatalf("session not found exit code = %d, want 4", got)
	}
	if got := ExitCode(processExitError{code: 2, err: process.ErrAmbiguous}); got != 2 {
		t.Fatalf("CLI main mapping returned %d for ambiguous selector", got)
	}
}

func shortCLIHome(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	repoRoot, err := filepath.Abs(filepath.Join(filepath.Dir(source), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(repoRoot, "tmp", "s6cli")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	home, err := os.MkdirTemp(root, "h-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	return home
}

func TestACPRejectsPositionalsWithoutWritingProtocolOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"acp", "extra"})
	assert.Err(t, err)
	assert.Eq(t, true, strings.Contains(err.Error(), "does not accept arguments"))
	assert.Eq(t, "", stdout.String())
}

func TestVersionFlagsWriteConfiguredOutputAndDoNotAffectNextApp(t *testing.T) {
	for _, flag := range []string{"--version", "-V"} {
		t.Run(flag, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{flag})
			assert.NoErr(t, err)
			assert.Eq(t, "suag dev (dev)\n", stdout.String())
			assert.Eq(t, "", stderr.String())
		})
	}

	var stdout, stderr bytes.Buffer
	err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"run", "--help"})
	assert.NoErr(t, err)
	assert.Eq(t, true, strings.Contains(stderr.String(), "--prompt"))
	assert.Eq(t, "", stdout.String())
}

func TestVersionCommandWritesOneLine(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"version"})
	assert.NoErr(t, err)
	assert.Eq(t, "suag dev (dev)\n", stdout.String())
	assert.Eq(t, "", stderr.String())
}

func TestSendWaitReportsQueuedFate(t *testing.T) {
	start := func(t *testing.T, sessionID string, settled chan error) {
		t.Helper()
		home := shortCLIHome(t)
		t.Setenv("SUAG_HOME", home)
		server, err := process.StartWait(filepath.Join(home, "run"), sessionID, "wait", t.TempDir(), func(text, from string) (bool, <-chan error, error) {
			return true, settled, nil
		})
		assert.Require(t, assert.NoErr(t, err))
		t.Cleanup(func() { _ = server.Close() })
	}
	t.Run("discarded message exits 3", func(t *testing.T) {
		settled := make(chan error, 1)
		settled <- process.ErrDiscarded
		start(t, "session-wait-a", settled)
		var stdout, stderr bytes.Buffer
		err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"send", "--session", "session-wait-a", "--text", "hi", "--wait"})
		assert.Eq(t, 3, ExitCode(err))
	})
	t.Run("started message reports delivered", func(t *testing.T) {
		settled := make(chan error, 1)
		settled <- nil
		start(t, "session-wait-b", settled)
		var stdout, stderr bytes.Buffer
		err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"send", "--session", "session-wait-b", "--text", "hi", "--wait"})
		assert.NoErr(t, err)
		assert.Eq(t, "delivered\n", stderr.String())
	})
	t.Run("without wait a queued message returns at once", func(t *testing.T) {
		start(t, "session-wait-c", make(chan error))
		var stdout, stderr bytes.Buffer
		err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"send", "--session", "session-wait-c", "--text", "hi"})
		assert.NoErr(t, err)
		assert.Eq(t, "queued\n", stderr.String())
	})
}

func TestSendRejectsOversizeStdinInsteadOfTruncating(t *testing.T) {
	var stdout, stderr bytes.Buffer
	big := strings.NewReader(strings.Repeat("a", maxSendStdinBytes+1))
	err := newCLI(context.Background(), big, &stdout, &stderr).RunWithArgs([]string{"send", "--session", "any"})
	assert.Require(t, assert.NotNil(t, err))
	assert.Eq(t, 4, ExitCode(err))
	assert.StrContains(t, err.Error(), "1 MiB")
}

func TestTraceCommandPrintsSessionTimeline(t *testing.T) {
	t.Setenv("SUAG_HOME", shortCLIHome(t))
	dbPath, err := appconfig.ResolveCoreStorePath()
	assert.NoErr(t, err)
	repo, err := store.Open(dbPath)
	assert.NoErr(t, err)
	ctx := context.Background()
	assert.NoErr(t, repo.CreateSession(ctx, store.Session{ID: "trace-s1", Status: store.SessionOpen}))
	for _, e := range []store.Event{
		{SessionID: "trace-s1", TurnID: "t1", Kind: store.EventTurn, Name: "turn", Status: "completed"},
		{SessionID: "trace-s1", TurnID: "t1", Kind: store.EventModel, Name: "primary", Status: "error", Error: "boom"},
		{SessionID: "trace-s1", TurnID: "t2", Kind: store.EventDenial, Name: "mcp_write", Status: "denied"},
	} {
		_, err := repo.AppendEvent(ctx, e)
		assert.NoErr(t, err)
	}
	assert.NoErr(t, repo.Close())

	run := func(args ...string) string {
		var stdout, stderr bytes.Buffer
		assert.NoErr(t, newCLI(ctx, strings.NewReader(""), &stdout, &stderr).RunWithArgs(args))
		return stdout.String()
	}
	t.Run("text", func(t *testing.T) {
		out := run("trace", "trace-s1")
		assert.Eq(t, true, strings.Contains(out, " model primary error "))
		assert.Eq(t, true, strings.Contains(out, "err=boom"))
		assert.Eq(t, true, strings.Contains(out, " denial mcp_write denied "))
	})
	t.Run("turn filter with json", func(t *testing.T) {
		out := run("trace", "trace-s1", "--turn", "t2", "--json")
		lines := strings.Split(strings.TrimSpace(out), "\n")
		assert.Eq(t, 1, len(lines))
		var row map[string]any
		assert.NoErr(t, json.Unmarshal([]byte(lines[0]), &row))
		assert.Eq(t, "denial", row["kind"])
	})
	t.Run("unknown session", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := newCLI(ctx, strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"trace", "nope"})
		assert.Err(t, err)
	})
}
