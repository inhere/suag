package hooks

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
)

func TestGoferHookFailuresAndJobEnvironmentAreBestEffort(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	t.Setenv("GOFER_JOB_ID", "")
	t.Setenv("SUAG_GOFER_HOOKS", "") // exercise the config value, not the TestMain override
	t.Setenv("PATH", t.TempDir())
	_ = gkconfig.Set("gofer.hooks", "auto")
	_ = gkconfig.Set("gofer.command", "missing-gofer-command-7cb7")
	if got := New(); got != nil {
		t.Fatal("auto mode should skip a missing command")
	}

	t.Setenv("GOFER_JOB_ID", "inside-job")
	_ = gkconfig.Set("gofer.hooks", "on")
	if got := New(); got != nil {
		t.Fatal("hooks must be skipped inside a Gofer job")
	}

	// A configured but failing hook process yields an empty response, never a panic.
	t.Setenv("GOFER_JOB_ID", "")
	h := &goferHooks{command: "", agent: "suag", timeout: time.Millisecond, waitSec: 1}
	assert.Eq(t, Response{}, h.Call(context.Background(), Event{Name: "SessionStart"}))
	result := <-h.StartStop(context.Background(), Event{Name: "Stop"}).Done()
	assert.Err(t, result.Err)
}

func TestHookPayloadCapsToolOutputAtUTF8Boundary(t *testing.T) {
	value := strings.Repeat("工", 8*1024)
	got := TruncateOutput(value, 8*1024)
	if len(got) > 8*1024 || !utf8.ValidString(got) {
		t.Fatalf("hook output is not valid bounded UTF-8: bytes=%d", len(got))
	}
}

func TestGoferStopInterruptIsSentBeforeStopProcessCancellation(t *testing.T) {
	var sequence []string
	done := make(chan Result, 1)
	done <- Result{}
	wait := &goferStopWait{
		call: func(_ context.Context, event Event) Response {
			if event.Name == "Interrupt" {
				sequence = append(sequence, "interrupt")
			}
			return Response{}
		},
		event:  Event{Name: "Stop", SessionID: "s1"},
		cancel: func() { sequence = append(sequence, "cancel") },
		done:   done,
	}
	wait.Interrupt(context.Background())
	if len(sequence) != 2 || sequence[0] != "interrupt" || sequence[1] != "cancel" {
		t.Fatalf("Stop process cancelled before Interrupt completed: %#v", sequence)
	}
}

func TestGoferStopInterruptReturnsAlreadyReceivedReply(t *testing.T) {
	done := make(chan Result, 1)
	done <- Result{Response: Response{Decision: "block", Reason: "[gofer web 回复] hi"}}
	wait := &goferStopWait{
		call:   func(context.Context, Event) Response { return Response{} },
		cancel: func() {},
		done:   done,
	}
	assert.Eq(t, "[gofer web 回复] hi", wait.Interrupt(context.Background()))
}

func TestGoferHooksCaptureStderrAsNotice(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a sh script as the fake gofer command")
	}
	script := filepath.Join(t.TempDir(), "fakegofer")
	assert.Require(t, assert.NoErr(t, os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\necho 'taken over on web' >&2\n"), 0o755)))
	h := &goferHooks{command: script, agent: "suag", timeout: 5 * time.Second, waitSec: 1}
	resp := h.Call(context.Background(), Event{Name: "UserPromptSubmit"})
	assert.Eq(t, "taken over on web", resp.Notice)
}
