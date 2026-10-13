package personal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/inhere/suga/internal/personal/process"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/redact"
	"github.com/inhere/suga/pkg/agentcore/tools"
)

// outputRefPrefix starts every ref to an externalised tool output:
// out:<session_id>:<16 hex>.
const outputRefPrefix = "out:"

// SessionSpiller stores oversized tool outputs as
// <sessions>/<session_id>/outputs/<hex>.txt (tools.Spiller). The session and
// turn come from the observe scope in ctx.
type SessionSpiller struct {
	dir string // the sessions root, e.g. ~/.suga/sessions
}

var _ tools.Spiller = (*SessionSpiller)(nil)

// NewSessionSpiller returns a spiller writing under sessionsDir.
func NewSessionSpiller(sessionsDir string) *SessionSpiller {
	return &SessionSpiller{dir: sessionsDir}
}

// OutputFileName is the file stem of one externalised output: the first 16
// hex digits of sha256(turn_id + "\x00" + call_id). The call id comes from the
// model and may hold ':', '/', '..' or an absolute path, so it never appears
// in the path itself.
func OutputFileName(turnID, callID string) string {
	sum := sha256.Sum256([]byte(turnID + "\x00" + callID))
	return hex.EncodeToString(sum[:8])
}

// SessionOutputsDir is the outputs directory of one session under sessionsDir.
func SessionOutputsDir(sessionsDir, sessionID string) string {
	return filepath.Join(sessionsDir, sessionID, "outputs")
}

// Spill writes content (secrets masked) and returns its ref. It fails without
// a session in ctx or with an invalid session id; the output layer then falls
// back to truncation.
func (s *SessionSpiller) Spill(ctx context.Context, call agentcore.ToolCall, content string) (string, error) {
	if s == nil || strings.TrimSpace(s.dir) == "" {
		return "", errors.New("spill: no sessions directory")
	}
	scope := observeScopeFrom(ctx)
	if scope.sessionID == "" {
		return "", errors.New("spill: no session in context")
	}
	if err := validateSessionID(scope.sessionID); err != nil {
		return "", fmt.Errorf("spill: %w", err)
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return "", fmt.Errorf("spill: create sessions directory: %w", err)
	}
	// Same protection as the run directory (0700 / protected DACL); the
	// session subdirectories created below inherit it on Windows.
	if err := process.SecureDir(s.dir); err != nil {
		return "", fmt.Errorf("spill: secure %s: %w", s.dir, err)
	}
	// Every path below is resolved inside the sessions root, so nothing
	// (symlinks included) can lead the write outside it.
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return "", fmt.Errorf("spill: %w", err)
	}
	defer root.Close()
	rel := filepath.Join(scope.sessionID, "outputs")
	if err := root.MkdirAll(rel, 0o700); err != nil {
		return "", fmt.Errorf("spill: create outputs directory: %w", err)
	}
	name := OutputFileName(scope.turnID, call.CallID)
	f, err := root.OpenFile(filepath.Join(rel, name+".txt"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("spill: %w", err)
	}
	if _, err := f.WriteString(redact.Secrets(content)); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("spill: write: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("spill: close: %w", err)
	}
	return outputRefPrefix + scope.sessionID + ":" + name, nil
}

// TaskOutput names the output file of background task taskID of a session
// (tasks.Config.Output) and its ref: <sessions>/<sid>/outputs/<hex>.txt, so
// recall and session_search read it like any saved output while it grows.
// The task writes it itself (redacted line by line).
func (s *SessionSpiller) TaskOutput(sessionID, taskID string) (string, string, error) {
	if s == nil || strings.TrimSpace(s.dir) == "" {
		return "", "", errors.New("task output: no sessions directory")
	}
	if err := validateSessionID(sessionID); err != nil {
		return "", "", fmt.Errorf("task output: %w", err)
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return "", "", fmt.Errorf("task output: create sessions directory: %w", err)
	}
	if err := process.SecureDir(s.dir); err != nil {
		return "", "", fmt.Errorf("task output: secure %s: %w", s.dir, err)
	}
	dir := SessionOutputsDir(s.dir, sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("task output: create outputs directory: %w", err)
	}
	name := OutputFileName("task\x00"+sessionID, taskID)
	return filepath.Join(dir, name+".txt"), outputRefPrefix + sessionID + ":" + name, nil
}
