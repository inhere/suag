package wiring

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"

	appconfig "github.com/inhere/suga/internal/config"
)

// logFileName is the file the process logger appends to under SUGA_HOME. It is
// opened once per process and never rotated: concurrent runs (a chat session and
// a Gofer job) append to the same file. Rotation and a config knob are left for
// later (issue suga-5dyx).
const logFileName = "suga.log"

// SetupLogging installs the process logger. Records go to
// <SUGA_HOME>/logs/suga.log and nowhere else in a regular run: what a person
// needs is printed by the run itself (the session id, the model, a retry, a
// wrap-up reason are plain notices on stderr) and Gofer reads stdout as
// protocol, so free-form log lines would only clutter the terminal or the job
// log. SUGA_DEBUG adds debug records and a copy on stderr — the mode to reach
// for when diagnosing a problem.
func SetupLogging() {
	_ = setupLogging(os.Stderr, os.Getenv("SUGA_DEBUG") != "")
}

// setupLogging is SetupLogging with the debug flag injected, so tests need
// neither the environment nor the process stderr. It returns the open log file
// (nil when it could not be opened): the process keeps it open for its lifetime,
// a test closes it before its temporary home goes away.
func setupLogging(stderr io.Writer, debugMode bool) *os.File {
	level := slog.LevelInfo
	if debugMode {
		level = slog.LevelDebug
	}

	writers := make([]io.Writer, 0, 2)
	logFile, logErr := openLogFile()
	if logErr != nil {
		// Nothing to write to but stderr: a log file that cannot be opened must
		// not turn into silence, so stderr keeps the records even though it is
		// otherwise quiet.
		writers = append(writers, stderr)
	} else {
		// The handler writes each record synchronously, so the open file needs
		// no flush at exit; it stays open for the process lifetime.
		writers = append(writers, logFile)
		if debugMode {
			writers = append(writers, stderr)
		}
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.MultiWriter(writers...), &slog.HandlerOptions{Level: level})))

	if logErr != nil {
		slog.Warn("cannot open the log file; logging to stderr only", "path", logPathOrEmpty(), "err", logErr)
	}
	if debugMode {
		slog.Debug("DEBUG MODE ENABLED - Verbose logging is active", "log_file", logFileOrEmpty(logFile))
	}
	return logFile
}

// openLogFile appends to <SUGA_HOME>/logs/suga.log, creating the directory with
// the permissions the rest of the data directory uses.
func openLogFile() (*os.File, error) {
	path, err := logFilePath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}

// logFilePath returns <SUGA_HOME>/logs/suga.log.
func logFilePath() (string, error) {
	home, err := appconfig.ResolveSugaHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "logs", logFileName), nil
}

// logPathOrEmpty is the log path for a message about a file that could not be
// opened; the path itself may be the reason, so a failure is reported empty.
func logPathOrEmpty() string {
	path, err := logFilePath()
	if err != nil {
		return ""
	}
	return path
}

// logFileOrEmpty names the open log file, or "" when it could not be opened.
func logFileOrEmpty(file *os.File) string {
	if file == nil {
		return ""
	}
	return file.Name()
}
