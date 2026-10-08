package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/personal/process"
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
	if got := commandExitCode(err); got != 3 {
		t.Fatalf("CLI main mapping returned %d for offline session", got)
	}
	if got := commandExitCode(processExitError{code: 2, err: process.ErrAmbiguous}); got != 2 {
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

func TestFormatVersionIncludesAvailableBuildMetadata(t *testing.T) {
	assert.Eq(t, "suag dev (dev)", formatVersion("dev", ""))
	assert.Eq(t, "suag 1.2.3 (abc123)", formatVersion("1.2.3", "abc123"))
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
		assert.Eq(t, 3, commandExitCode(err))
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
	assert.Eq(t, 4, commandExitCode(err))
	assert.StrContains(t, err.Error(), "1 MiB")
}
