package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/mock"
)

func TestPersonalCLIUsesIsolatedHomeAcrossRuns(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SUAG_HOME", "") // fall back to HOME/.suag despite the TestMain default
	t.Setenv("SUAG_MODEL", "environment-model")
	// Same vendor as llm.api_type: switching vendors drops the file's base_url/api_key,
	// which would send this run to the real vendor instead of the test server.
	t.Setenv("SUAG_PROVIDER", "openai")
	workspace := t.TempDir()
	t.Chdir(workspace)
	_ = gkconfig.Set("workspace", workspace)
	_ = gkconfig.Set("llm.api_type", "openai")
	_ = gkconfig.Set("llm.api_key", "local-test-key")
	var requestCount atomic.Int32
	var requestedModels []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected provider path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		requestedModels = append(requestedModels, body.Model)
		n := requestCount.Add(1)
		if n == 2 {
			foundPreviousAnswer := false
			for _, message := range body.Messages {
				if message.Role == "assistant" && message.Content == "reply-1" {
					foundPreviousAnswer = true
				}
			}
			if !foundPreviousAnswer {
				t.Error("second turn omitted persisted assistant context")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":"reply-%d"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`, n)
	}))
	t.Cleanup(server.Close)
	_ = gkconfig.Set("llm.base_url", server.URL)

	var firstOut, firstErr bytes.Buffer
	err := RunPersonalCLI(context.Background(), "run", []string{"--prompt", "hello", "--output-format", "stream-json", "--read-only", "--model", "flag-model", "--provider", "openai", "--session-id", "path-test"},
		strings.NewReader(""), &firstOut, &firstErr)
	assert.NoErr(t, err)
	assert.Eq(t, true, strings.Contains(firstOut.String(), `"result":"reply-1"`))
	assert.Eq(t, true, strings.Contains(firstOut.String(), `"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}`))
	var secondOut, secondErr bytes.Buffer
	err = RunPersonalCLI(context.Background(), "run", []string{"--prompt", "follow up", "--output-format", "stream-json", "--read-only", "--resume", "path-test"},
		strings.NewReader(""), &secondOut, &secondErr)
	assert.NoErr(t, err)
	assert.Eq(t, true, strings.Contains(secondOut.String(), `"result":"reply-2"`))
	assert.Eq(t, int32(2), requestCount.Load())
	assert.Eq(t, []string{"flag-model", "environment-model"}, requestedModels)
	transcript, err := os.ReadFile(filepath.Join(home, ".suag", "transcripts", "path-test.jsonl"))
	assert.NoErr(t, err)
	assert.Eq(t, true, strings.Contains(string(transcript), "hello"))
	assert.Eq(t, true, strings.Contains(string(transcript), "reply-2"))

	personalDB := filepath.Join(home, ".suag", "db", "sessions.db")
	_, err = os.Stat(personalDB)
	assert.NoErr(t, err)
	repo, err := store.Open(personalDB)
	assert.NoErr(t, err)
	defer repo.Close()
	stored, err := repo.GetSession(context.Background(), "path-test")
	assert.NoErr(t, err)
	assertOnlySessionsDB(t, filepath.Join(home, ".suag", "db"))
	turns, err := repo.ListTurns(context.Background(), "path-test", store.TurnCompleted)
	assert.NoErr(t, err)
	assert.Eq(t, 2, len(turns))
	assert.Eq(t, "environment-model", stored.Meta["model"])
	assert.Eq(t, "openai", stored.Meta["provider"])
	_, err = os.Stat(filepath.Join(workspace, "db"))
	assert.Eq(t, true, errors.Is(err, os.ErrNotExist))
}

func TestPersonalCLIInjectsRulesPromptAndGoferHint(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SUAG_HOME", "") // fall back to HOME/.suag despite the TestMain default
	suagHome := t.TempDir()
	t.Setenv("SUAG_HOME", suagHome)
	t.Setenv("SUAG_APPEND_SYSTEM_PROMPT", "environment prompt")
	t.Setenv("GOFER_JOB_ID", "test-job")
	if err := os.WriteFile(filepath.Join(suagHome, "AGENTS.md"), []byte("user-level rule"), 0644); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("project-level rule"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workspace)
	_ = gkconfig.Set("workspace", workspace)
	_ = gkconfig.Set("llm.api_type", "openai")
	_ = gkconfig.Set("llm.api_key", "local-test-key")
	var systems []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		for _, message := range body.Messages {
			if message.Role == "system" {
				systems = append(systems, message.Content)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(server.Close)
	_ = gkconfig.Set("llm.base_url", server.URL)
	for _, args := range [][]string{
		{"--prompt", "one", "--session-id", "flags", "--append-system-prompt", "flag prompt"},
		{"--prompt", "two", "--session-id", "environment", "--no-project-rules"},
	} {
		var stdout, stderr bytes.Buffer
		if err := RunPersonalCLI(context.Background(), "run", args, strings.NewReader(""), &stdout, &stderr); err != nil {
			t.Fatal(err)
		}
	}
	assert.Eq(t, 2, len(systems))
	assert.Eq(t, true, strings.Contains(systems[0], "user-level rule"))
	assert.Eq(t, true, strings.Contains(systems[0], "project-level rule"))
	assert.Eq(t, true, strings.Contains(systems[0], "flag prompt"))
	assert.Eq(t, false, strings.Contains(systems[0], "environment prompt"))
	assert.Eq(t, true, strings.Contains(systems[0], "gofer work report"))
	assert.Eq(t, true, strings.Contains(systems[1], "user-level rule"))
	assert.Eq(t, false, strings.Contains(systems[1], "project-level rule"))
	assert.Eq(t, true, strings.Contains(systems[1], "environment prompt"))
}

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
	assert.Eq(t, true, errors.Is(err, personal.ErrSessionNotFound))
	assert.Eq(t, "", stdout.String())
	sessions, err := repo.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, 0, len(sessions))
}

func TestPersonalChatReusesOneSession(t *testing.T) {
	runner, repo := newCLITestRunner(t, mock.NewProvider(mock.Final("first"), mock.Final("second")))
	var stdout, stderr bytes.Buffer
	err := runPersonalChatWithHooks(context.Background(), runner, personalOptions{OutputFormat: "text"},
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

func TestParsePersonalOptionsRejectsInvalidCombinations(t *testing.T) {
	var stderr bytes.Buffer
	err := RunPersonalCLI(context.Background(), "run", []string{"--prompt", "hi", "--session-id", "one", "--resume", "two"},
		strings.NewReader(""), &bytes.Buffer{}, &stderr)
	assert.Eq(t, true, err != nil && strings.Contains(err.Error(), "cannot be combined"))
	err = RunPersonalCLI(context.Background(), "run", []string{"--prompt", "hi", "--output-format", "xml"},
		strings.NewReader(""), &bytes.Buffer{}, &stderr)
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

// newTestStorePath returns a fresh core store path under the test's temp dir.
func newTestStorePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "db", "sessions.db")
}

func TestPersonalFromHandoff(t *testing.T) {
	t.Run("option conflicts", func(t *testing.T) {
		err := RunPersonalCLI(context.Background(), "run", []string{"--prompt", "hi", "--from", "a", "--resume", "b"},
			strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
		assert.Eq(t, true, err != nil && strings.Contains(err.Error(), "--from and --resume cannot be combined"))
	})
	runner, repo := newCLITestRunner(t, mock.NewProvider(mock.Final("ok"), mock.Final("ok2")))
	ctx := context.Background()
	t.Run("unknown source behaves like unknown resume", func(t *testing.T) {
		var stdout bytes.Buffer
		err := executePersonalRun(ctx, runner, personalOptions{Prompt: "go", FromID: "missing", OutputFormat: "stream-json"}, &stdout, &bytes.Buffer{})
		assert.Eq(t, true, errors.Is(err, personal.ErrSessionNotFound))
		assert.Eq(t, "", stdout.String())
	})
	// A manually written compaction lets the handoff reuse its summary offline.
	src, err := runner.NewSession(ctx, "src-1", "src")
	assert.NoErr(t, err)
	_, err = repo.AppendEntries(ctx, src.ID, store.NewEntry{Type: store.EntryCompaction, JSON: []byte(`{"summary":"OLD-SUMMARY","first_kept_seq":1}`)})
	assert.NoErr(t, err)
	t.Run("existing --session-id is rejected", func(t *testing.T) {
		err := executePersonalRun(ctx, runner, personalOptions{Prompt: "go", FromID: "src-1", SessionID: "src-1", OutputFormat: "stream-json"}, &bytes.Buffer{}, &bytes.Buffer{})
		assert.Eq(t, true, err != nil && strings.Contains(err.Error(), "already exists"))
	})
	t.Run("new session id is reported and carries the handoff", func(t *testing.T) {
		var stdout bytes.Buffer
		err := executePersonalRun(ctx, runner, personalOptions{Prompt: "go", FromID: "src-1", SessionID: "child-1", OutputFormat: "stream-json"}, &stdout, &bytes.Buffer{})
		assert.NoErr(t, err)
		var first map[string]any
		assert.NoErr(t, json.Unmarshal([]byte(strings.SplitN(stdout.String(), "\n", 2)[0]), &first))
		assert.Eq(t, "child-1", first["id"])
		e, err := repo.LatestEntry(ctx, "child-1", store.EntryHandoff)
		assert.NoErr(t, err)
		assert.Eq(t, true, e != nil && strings.Contains(string(e.JSON), "OLD-SUMMARY"))
	})
}
