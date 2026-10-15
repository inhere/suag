package wiring

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// TestSetupLoggingIsQuietOutsideTheFile: a regular run writes its records to the
// log file only — stderr stays for the notices the run prints itself.
func TestSetupLoggingIsQuietOutsideTheFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SUGA_HOME", home)
	restoreDefaultLogger(t)

	var stderr bytes.Buffer
	closeLogFile(t, setupLogging(&stderr, false))
	slog.Warn("llm request failed, retrying", "provider", "test")

	data, err := os.ReadFile(filepath.Join(home, "logs", logFileName))
	assert.Require(t, assert.NoErr(t, err))
	assert.StrContains(t, string(data), "level=WARN")
	assert.StrContains(t, string(data), "llm request failed, retrying")
	assert.StrContains(t, string(data), "provider=test")
	assert.Eq(t, "", stderr.String())
}

// TestSetupLoggingDebugAlsoWritesStderr: SUGA_DEBUG is the diagnosing mode, so
// the records also reach stderr and name the log file.
func TestSetupLoggingDebugAlsoWritesStderr(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SUGA_HOME", home)
	t.Setenv("SUGA_DEBUG", "1")
	restoreDefaultLogger(t)

	var stderr bytes.Buffer
	closeLogFile(t, setupLogging(&stderr, os.Getenv("SUGA_DEBUG") != ""))
	slog.Debug("debug record")

	logPath := filepath.Join(home, "logs", logFileName)
	data, err := os.ReadFile(logPath)
	assert.Require(t, assert.NoErr(t, err))
	assert.StrContains(t, string(data), "DEBUG MODE ENABLED")
	assert.StrContains(t, string(data), "debug record")
	assert.StrContains(t, string(data), "log_file="+logPath)
	assert.StrContains(t, stderr.String(), "debug record")
}

// TestSetupLoggingFallsBackToStderr: a log file that cannot be opened must not
// silence the logs, so stderr keeps them even though it is otherwise quiet.
func TestSetupLoggingFallsBackToStderr(t *testing.T) {
	// A file where the log directory should be: <SUGA_HOME>/logs cannot be created.
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	assert.Require(t, assert.NoErr(t, os.WriteFile(blocker, []byte("x"), 0o600)))
	t.Setenv("SUGA_HOME", blocker)
	restoreDefaultLogger(t)

	var stderr bytes.Buffer
	closeLogFile(t, setupLogging(&stderr, false))
	slog.Warn("still visible")

	assert.StrContains(t, stderr.String(), "cannot open the log file")
	assert.StrContains(t, stderr.String(), "still visible")
}

// closeLogFile closes the log file a test opened. The process keeps it open for
// its lifetime; on Windows an open file blocks the temporary home's cleanup.
func closeLogFile(t *testing.T, file *os.File) {
	t.Helper()
	if file == nil {
		return
	}
	t.Cleanup(func() { _ = file.Close() })
}

// restoreDefaultLogger puts the previous default logger back so the rest of the
// package keeps the logger it started with.
func restoreDefaultLogger(t *testing.T) {
	t.Helper()
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
}
