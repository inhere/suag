package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	assert.NoErr(t, os.MkdirAll(filepath.Dir(p), 0o755))
	assert.NoErr(t, os.WriteFile(p, []byte(s), 0o644))
}

func TestLoadTaskNewFields(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "task.yaml"), `name: x
prompt: hi
timeout: 20m
acceptance: acc
checks:
  - {type: command, args: [go, version], timeout: 1m}
`)
	task, err := loadTask(dir)
	assert.NoErr(t, err)
	assert.Eq(t, 20*time.Minute, task.effectiveTimeout(time.Minute))
	assert.Eq(t, "acc", task.Acceptance)
	assert.Eq(t, []string{"go", "version"}, task.Checks[0].Args)

	writeFile(t, filepath.Join(dir, "task.yaml"), "name: x\nprompt: hi\n")
	task, err = loadTask(dir)
	assert.NoErr(t, err)
	assert.Eq(t, time.Minute, task.effectiveTimeout(time.Minute))

	writeFile(t, filepath.Join(dir, "task.yaml"), "name: x\nprompt: hi\ntimeout: soon\n")
	_, err = loadTask(dir)
	assert.Err(t, err)
	writeFile(t, filepath.Join(dir, "task.yaml"), "name: x\nprompt: hi\nchecks:\n  - {type: command}\n")
	_, err = loadTask(dir)
	assert.Err(t, err)
	writeFile(t, filepath.Join(dir, "task.yaml"), "name: x\nprompt: hi\nacceptance: ../out\n")
	_, err = loadTask(dir)
	assert.Err(t, err)
}

func TestLongCodingTaskLoads(t *testing.T) {
	task, err := loadTask(filepath.Join("testdata", "tasks", "long-coding-schedule"))
	assert.NoErr(t, err)
	assert.Eq(t, 25*time.Minute, task.effectiveTimeout(time.Minute))
	_, err = os.Stat(filepath.Join(task.dir, task.Acceptance, "schedule", "zz_acceptance_test.go"))
	assert.NoErr(t, err)
}

func TestAcceptanceInjectionAndCommandCheck(t *testing.T) {
	taskDir, ws := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(taskDir, "acc", "sub", "a.txt"), "from-acceptance")
	writeFile(t, filepath.Join(ws, "sub", "a.txt"), "agent-version")
	writeFile(t, filepath.Join(ws, "keep.txt"), "keep")
	task := &Task{
		dir:        taskDir,
		Acceptance: "acc",
		Checks: []Check{
			{Type: "file_equals", Path: "sub/a.txt", Text: "from-acceptance"},
			{Type: "file_contains", Path: "keep.txt", Text: "keep"},
			{Type: "command", Args: []string{"go", "env", "GOOS"}},
			{Type: "command", Args: []string{"go", "nosuchsubcommand"}},
			{Type: "command", Args: []string{"definitely-not-a-binary-xyz"}},
		},
	}
	var r Result
	task.finish(ws, "", &r)
	assert.Eq(t, 5, len(r.Checks))
	assert.True(t, r.Checks[0].Passed)
	assert.True(t, r.Checks[1].Passed)

	ok := r.Checks[2]
	assert.True(t, ok.Passed)
	assert.Eq(t, 0, *ok.ExitCode)
	assert.Eq(t, runtime.GOOS, ok.Output)
	assert.Eq(t, "go env GOOS", ok.Text)

	bad := r.Checks[3]
	assert.False(t, bad.Passed)
	assert.True(t, *bad.ExitCode > 0)
	assert.True(t, strings.Contains(bad.Output, "nosuchsubcommand"))

	missing := r.Checks[4]
	assert.False(t, missing.Passed)
	assert.Eq(t, -1, *missing.ExitCode)
	assert.NotEmpty(t, missing.Detail)
	assert.False(t, allPassed(r.Checks))
}

func TestCommandCheckTimeoutAndOutputTail(t *testing.T) {
	ws := t.TempDir()
	r := runCommandCheck(ws, Check{Type: "command", Args: []string{"go", "version"}, Timeout: "1ns"})
	assert.False(t, r.Passed)
	assert.Eq(t, -1, *r.ExitCode)

	s := tailString(strings.Repeat("é", 5000), 100)
	assert.True(t, strings.HasPrefix(s, "..."))
	assert.True(t, len(s) <= 103)
}

func TestAcceptanceMissingDirFails(t *testing.T) {
	task := &Task{dir: t.TempDir(), Acceptance: "nope"}
	var r Result
	task.finish(t.TempDir(), "", &r)
	assert.False(t, allPassed(r.Checks))
}
