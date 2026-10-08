package app

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
	appconfig "github.com/inhere/suag/internal/config"
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

func TestContextWindowForSettingsFollowsEffectiveModel(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	_ = gkconfig.Set("llm.api_type", "openai")
	_ = gkconfig.Set("llm.model", "gpt-4o")
	t.Setenv("SUAG_MODEL", "")
	t.Run("file model", func(t *testing.T) {
		assert.Eq(t, 128000, contextWindowForSettings(appconfig.ReadLLMProviderSettingsWithOverrides("", "")))
	})
	t.Run("flag overrides file model", func(t *testing.T) {
		assert.Eq(t, 1000000, contextWindowForSettings(appconfig.ReadLLMProviderSettingsWithOverrides("gpt-4.1", "")))
	})
	t.Run("env overrides file model", func(t *testing.T) {
		t.Setenv("SUAG_MODEL", "gpt-5")
		assert.Eq(t, 400000, contextWindowForSettings(appconfig.ReadLLMProviderSettingsWithOverrides("", "")))
	})
	t.Run("configured context_window wins", func(t *testing.T) {
		_ = gkconfig.Set("llm.context_window", 64000)
		assert.Eq(t, 64000, contextWindowForSettings(appconfig.ReadLLMProviderSettingsWithOverrides("gpt-4.1", "")))
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
