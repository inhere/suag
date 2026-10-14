package wiring

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// TestSetupLoggingChannels covers the two destinations of a record: the log file
// always, stderr only when it is not a terminal.
func TestSetupLoggingChannels(t *testing.T) {
	cases := []struct {
		name             string
		stderrIsTerminal bool
		wantStderr       bool
	}{
		{name: "a terminal keeps stderr clean", stderrIsTerminal: true, wantStderr: false},
		{name: "a pipe also gets stderr", stderrIsTerminal: false, wantStderr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("SUGA_HOME", home)
			t.Setenv("SUGA_DEBUG", "")
			restoreDefaultLogger(t)

			var stderr bytes.Buffer
			closeLogFile(t, setupLogging(&stderr, tc.stderrIsTerminal))
			slog.Warn("llm request failed, retrying", "provider", "test")

			data, err := os.ReadFile(filepath.Join(home, "logs", logFileName))
			assert.Require(t, assert.NoErr(t, err))
			assert.StrContains(t, string(data), "level=WARN")
			assert.StrContains(t, string(data), "llm request failed, retrying")
			assert.StrContains(t, string(data), "provider=test")

			if tc.wantStderr {
				assert.StrContains(t, stderr.String(), "llm request failed, retrying")
			} else {
				assert.Eq(t, "", stderr.String())
			}
		})
	}
}

// TestSetupLoggingFallsBackToStderr: a log file that cannot be opened must not
// silence the logs, so stderr keeps them even when it is a terminal.
func TestSetupLoggingFallsBackToStderr(t *testing.T) {
	// A file where the log directory should be: <SUGA_HOME>/logs cannot be created.
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	assert.Require(t, assert.NoErr(t, os.WriteFile(blocker, []byte("x"), 0o600)))
	t.Setenv("SUGA_HOME", blocker)
	t.Setenv("SUGA_DEBUG", "")
	restoreDefaultLogger(t)

	var stderr bytes.Buffer
	closeLogFile(t, setupLogging(&stderr, true))
	slog.Warn("still visible")

	assert.StrContains(t, stderr.String(), "cannot open the log file")
	assert.StrContains(t, stderr.String(), "still visible")
}

// TestSetupLoggingDebugNamesTheLogFile: SUGA_DEBUG adds debug records and tells
// where the log file is, so a person can find the file a terminal run hides.
func TestSetupLoggingDebugNamesTheLogFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SUGA_HOME", home)
	t.Setenv("SUGA_DEBUG", "1")
	restoreDefaultLogger(t)

	var stderr bytes.Buffer
	closeLogFile(t, setupLogging(&stderr, false))
	slog.Debug("debug record")

	data, err := os.ReadFile(filepath.Join(home, "logs", logFileName))
	assert.Require(t, assert.NoErr(t, err))
	assert.StrContains(t, string(data), "DEBUG MODE ENABLED")
	assert.StrContains(t, string(data), "debug record")
	assert.StrContains(t, string(data), "log_file="+filepath.Join(home, "logs", logFileName))
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
