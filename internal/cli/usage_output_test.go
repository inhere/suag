package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/mock"
)

// cachedUsageProvider reports provider-normalized usage where input_tokens
// includes the cache-read tokens (as the OpenAI and Claude providers do).
type cachedUsageProvider struct{}

func (cachedUsageProvider) Name() string { return "cached-usage" }

func (cachedUsageProvider) Generate(context.Context, *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	return &agentcore.GenerateResult{
		Content: "done", FinishReason: agentcore.FinishFinal,
		Usage: agentcore.Usage{InputTokens: 14, OutputTokens: 6, TotalTokens: 20, CacheReadTokens: 4},
	}, nil
}

type yieldAfterModelHook struct{ agentcore.NopHook }

func (yieldAfterModelHook) AfterModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	ctrl.Yield("input_required", nil)
	return nil
}

func lastJSONLine(t *testing.T, out string) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var line map[string]any
	assert.NoErr(t, json.Unmarshal([]byte(lines[len(lines)-1]), &line))
	return line
}

func TestStreamJSONUsageUsesAnthropicAccounting(t *testing.T) {
	want := map[string]any{"input_tokens": float64(10), "output_tokens": float64(6), "cache_read_tokens": float64(4), "total_tokens": float64(20)}
	t.Run("result line", func(t *testing.T) {
		runner, _ := newCLITestRunner(t, cachedUsageProvider{})
		var stdout, stderr bytes.Buffer
		assert.NoErr(t, executePersonalRun(context.Background(), runner, personalOptions{Prompt: "x", OutputFormat: "stream-json"}, &stdout, &stderr))
		line := lastJSONLine(t, stdout.String())
		assert.Eq(t, "result", line["type"])
		assert.Eq(t, want, line["usage"])
		assert.Eq(t, want, line["session_usage"])
	})
	t.Run("input_required line", func(t *testing.T) {
		runner, _ := newCLITestRunner(t, cachedUsageProvider{}, yieldAfterModelHook{})
		var stdout, stderr bytes.Buffer
		err := executePersonalRun(context.Background(), runner, personalOptions{Prompt: "x", OutputFormat: "stream-json"}, &stdout, &stderr)
		assert.Eq(t, true, errors.Is(err, ErrInputRequired))
		line := lastJSONLine(t, stdout.String())
		assert.Eq(t, "input_required", line["type"])
		assert.Eq(t, want, line["usage"])
		assert.Eq(t, want, line["session_usage"])
	})
	t.Run("error line carries usage fields", func(t *testing.T) {
		runner, _ := newCLITestRunner(t, mock.NewProvider(mock.Error(errors.New("boom"))))
		var stdout, stderr bytes.Buffer
		err := executePersonalRun(context.Background(), runner, personalOptions{Prompt: "x", OutputFormat: "stream-json"}, &stdout, &stderr)
		assert.Eq(t, true, err != nil)
		line := lastJSONLine(t, stdout.String())
		assert.Eq(t, "error", line["type"])
		assert.Eq(t, map[string]any{"input_tokens": float64(0), "output_tokens": float64(0), "total_tokens": float64(0)}, line["usage"])
		assert.Eq(t, true, line["session_usage"] != nil)
	})
}

// End to end through the real flag parser: an explicit empty value (both
// "--flag ”" and "--flag=") clears the saved append prompt; an absent flag keeps it.
func TestAppendSystemPromptCanBeClearedFromCLI(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	t.Setenv("SUAG_APPEND_SYSTEM_PROMPT", "")
	t.Setenv("SUAG_MODEL", "")
	t.Setenv("SUAG_PROVIDER", "openai")
	home := shortTestHome(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SUAG_HOME", "") // fall back to HOME/.suag despite the TestMain default
	workspace := t.TempDir()
	t.Chdir(workspace)
	_ = gkconfig.Set("workspace", workspace)
	_ = gkconfig.Set("llm.api_type", "openai")
	_ = gkconfig.Set("llm.api_key", "local-test-key")
	_ = gkconfig.Set("llm.model", "m")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`)
	}))
	t.Cleanup(server.Close)
	_ = gkconfig.Set("llm.base_url", server.URL)

	run := func(extra ...string) {
		t.Helper()
		args := append([]string{"--prompt", "hi", "--read-only", "--session-id", "clear-cli"}, extra...)
		assert.NoErr(t, RunPersonalCLI(context.Background(), "run", args, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}))
	}
	stored := func() any {
		t.Helper()
		repo, err := store.Open(filepath.Join(home, ".suag", "db", "sessions.db"))
		assert.NoErr(t, err)
		defer repo.Close()
		model, err := repo.GetSession(context.Background(), "clear-cli")
		assert.NoErr(t, err)
		return model.Meta["personal_append_prompt"]
	}
	run("--append-system-prompt", "saved")
	assert.Eq(t, "saved", stored())
	run() // absent flag keeps the saved prompt
	assert.Eq(t, "saved", stored())
	run("--append-system-prompt", "") // empty keeps it: Gofer renders an unset system prompt as ""
	assert.Eq(t, "saved", stored())
	run("--clear-append-system-prompt")
	assert.Eq(t, "", stored())
	run("--append-system-prompt", "again")
	assert.Eq(t, "again", stored())
	run("--clear-append-system-prompt", "--append-system-prompt", "") // the Gofer argv shape still clears
	assert.Eq(t, "", stored())
}

// Gofer contract: a missing --resume target or --from source exits 4 with a
// "session not found: <id>" message, distinct from other run failures.
func TestMissingSessionIsStableError(t *testing.T) {
	for _, opts := range []personalOptions{
		{Prompt: "x", ResumeID: "gone-session"},
		{Prompt: "x", FromID: "gone-session", SessionID: "fresh-child"},
	} {
		runner, _ := newCLITestRunner(t, cachedUsageProvider{})
		err := executePersonalRun(context.Background(), runner, opts, &bytes.Buffer{}, &bytes.Buffer{})
		var notFound personal.SessionNotFoundError
		assert.Eq(t, true, errors.As(err, &notFound))
		assert.Eq(t, 4, notFound.ExitCode())
		assert.Eq(t, "session not found: gone-session", err.Error())
		assert.Eq(t, true, errors.Is(err, store.ErrSessionNotFound))
	}
}
