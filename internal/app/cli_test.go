package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/data/testutil"
	"github.com/inhere/suag/internal/host/session"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/mock"
)

func newCLITestRunner(t *testing.T, provider agentcore.Provider, hooks ...agentcore.Hook) (*personal.Runner, *session.SQLiteRepository) {
	t.Helper()
	dbPath := testutil.NewMigratedSQLitePath(t, "sessions.db")
	repo, err := session.NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	runner, err := personal.New(personal.Config{
		Repository: repo,
		RunRoot:    filepath.Join(t.TempDir(), "runs"),
		Provider:   provider,
		Hooks:      hooks,
	})
	if err != nil {
		t.Fatal(err)
	}
	return runner, repo
}

func TestExecutePersonalRunEmitsGoferReadableStream(t *testing.T) {
	runner, _ := newCLITestRunner(t, mock.NewProvider(mock.Stream(mock.Final("done"),
		agentcore.StreamEvent{Kind: agentcore.StreamEventTextDelta, Delta: "do"},
	)))
	var stdout, stderr bytes.Buffer
	err := executePersonalRun(context.Background(), runner, personalOptions{
		Prompt: "say done", SessionID: "gofer-001", OutputFormat: "stream-json",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	assert.Eq(t, "session", events[0]["type"])
	assert.Eq(t, "gofer-001", events[0]["id"])
	assert.Eq(t, "result", events[len(events)-1]["type"])
	assert.Eq(t, "done", events[len(events)-1]["result"])
	var deltas, results int
	for _, event := range events {
		if event["type"] == "message_delta" {
			deltas++
		}
		if event["type"] == "result" {
			results++
		}
	}
	assert.Eq(t, 1, deltas)
	assert.Eq(t, 1, results)
	assert.Eq(t, "", stderr.String())
}

func TestExecutePersonalRunResumeRejectsUnknownSession(t *testing.T) {
	runner, repo := newCLITestRunner(t, mock.NewProvider(mock.Final("unused")))
	var stdout, stderr bytes.Buffer
	opts := personalOptions{Prompt: "continue", ResumeID: "missing", OutputFormat: "stream-json"}
	err := executePersonalRun(context.Background(), runner, opts, &stdout, &stderr)
	assert.Eq(t, true, errors.Is(err, session.ErrSessionNotFound))
	assert.Eq(t, "", stdout.String())
	sessions, err := repo.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, 0, len(sessions))
}

func TestPersonalReadOnlyToolsCannotWrite(t *testing.T) {
	workspace := t.TempDir()
	capabilities, err := buildPersonalCapabilities(workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	schemas, err := capabilities.Registry.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, 2, len(schemas))
	_, err = capabilities.Registry.ExecuteTool(context.Background(), agentcore.ToolCall{
		CallID: "write", Name: "file_write", Arguments: map[string]any{"path": "blocked.txt", "content": "secret"},
	})
	assert.Eq(t, true, err != nil)
	_, err = os.Stat(filepath.Join(workspace, "blocked.txt"))
	assert.Eq(t, true, errors.Is(err, os.ErrNotExist))
}

func TestPersonalChatReusesOneSession(t *testing.T) {
	runner, repo := newCLITestRunner(t, mock.NewProvider(mock.Final("first"), mock.Final("second")))
	var stdout, stderr bytes.Buffer
	err := runPersonalChat(context.Background(), runner, personalOptions{OutputFormat: "text"},
		strings.NewReader("Hello\nFollow up\n:quit\n"), &stdout, &stderr)
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
	events, err := repo.ListEventsBySession(context.Background(), sessions[0].SessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	runs := make(map[string]bool)
	for _, event := range events {
		runs[event.RunID] = true
	}
	assert.Eq(t, 2, len(runs))
}

func TestParsePersonalOptionsRejectsInvalidCombinations(t *testing.T) {
	var stderr bytes.Buffer
	_, err := parsePersonalOptions("run", []string{"--prompt", "hi", "--session-id", "one", "--resume", "two"}, &stderr)
	assert.Eq(t, true, err != nil && strings.Contains(err.Error(), "cannot be combined"))
	_, err = parsePersonalOptions("run", []string{"--prompt", "hi", "--output-format", "xml"}, &stderr)
	assert.Eq(t, true, err != nil && strings.Contains(err.Error(), "unsupported output format"))
}

type cliYieldHook struct{ agentcore.NopHook }

func (cliYieldHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	ctrl.Yield("input_required", nil)
	return nil
}

func TestExecutePersonalRunReportsWaitingWithoutFinalAnswer(t *testing.T) {
	runner, _ := newCLITestRunner(t, mock.NewProvider(mock.Final("unused")), cliYieldHook{})
	var stdout, stderr bytes.Buffer
	err := executePersonalRun(context.Background(), runner, personalOptions{
		Prompt: "wait for me", OutputFormat: "stream-json",
	}, &stdout, &stderr)
	assert.Eq(t, true, errors.Is(err, ErrInputRequired))
	assert.Eq(t, true, strings.Contains(stdout.String(), `"type":"input_required"`))
	assert.Eq(t, false, strings.Contains(stdout.String(), `"type":"result"`))
}

func TestPersonalHelpDoesNotOpenRuntime(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := RunPersonalCLI(context.Background(), "run", []string{"--help"},
		strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, true, strings.Contains(stderr.String(), "-prompt"))
}
