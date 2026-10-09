package chat

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/mock"
)

// shortTestHome returns a short SUAG_HOME (unix socket paths have a length limit).
func shortTestHome(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	repoRoot, err := filepath.Abs(filepath.Join(filepath.Dir(source), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(repoRoot, "tmp", "s6test")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(root, "h-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// newCLITestRunner builds a runner on a throwaway store (same helper cli tests use).
func newCLITestRunner(t *testing.T, provider agentcore.Provider, hooks ...agentcore.Hook) (*personal.Runner, *store.Store) {
	t.Helper()
	repo, err := store.Open(newTestStorePath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	runner, err := personal.New(personal.Config{
		Repository: repo,
		Provider:   provider,
		Hooks:      hooks,
	})
	if err != nil {
		t.Fatal(err)
	}
	return runner, repo
}

func TestPersonalChatReusesOneSession(t *testing.T) {
	runner, repo := newCLITestRunner(t, mock.NewProvider(mock.Final("first"), mock.Final("second")))
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), runner, Options{},
		strings.NewReader("Hello\nFollow up\n:quit\n"), &stdout, &stderr, nil)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := repo.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, 1, len(sessions))
	assert.Eq(t, true, strings.Contains(stdout.String(), "first"))
	assert.Eq(t, true, strings.Contains(stdout.String(), "second"))
	// Two prompts in one chat session are two completed turns of that session.
	turns, err := repo.ListTurns(context.Background(), sessions[0].ID, store.TurnCompleted)
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, 2, len(turns))
}

// newTestStorePath returns a fresh core store path under the test's temp dir.
func newTestStorePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "db", "sessions.db")
}
