package wiring

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/internal/personal"
)

func TestBasePromptDebugNonce(t *testing.T) {
	t.Run("unset has no nonce", func(t *testing.T) {
		t.Setenv(debugPromptNonceEnv, "")
		p := basePrompt(Options{Workspace: "/w"}, "")
		assert.Eq(t, false, strings.Contains(p, "nonce:"))
		assert.Eq(t, true, strings.HasPrefix(p, "You are suag"))
	})
	t.Run("set goes first", func(t *testing.T) {
		t.Setenv(debugPromptNonceEnv, "abc123")
		p := basePrompt(Options{Workspace: "/w"}, "")
		assert.Eq(t, true, strings.HasPrefix(p, "nonce: abc123\nYou are suag"))
	})
}

func TestBasePrompt(t *testing.T) {
	t.Run("read-only names the enable hint for the entry", func(t *testing.T) {
		p := basePrompt(Options{Workspace: "/w", CLIMode: true}, "")
		assert.StrContains(t, p, "Mode: read-only")
		assert.StrContains(t, p, "加 --allow-write")
		assert.StrContains(t, p, "Workspace (file tools are confined to it): /w")
		assert.NotContains(t, p, "Shell for shell_exec")
	})
	t.Run("writable names the shell", func(t *testing.T) {
		p := basePrompt(Options{Workspace: "/w", AllowWrite: true}, "")
		assert.StrContains(t, p, "Mode: writable")
		assert.StrContains(t, p, "Shell for shell_exec: ")
	})
	t.Run("scope discipline and stale-rule reporting", func(t *testing.T) {
		p := basePrompt(Options{Workspace: "/w"}, "")
		assert.StrContains(t, p, `"Found, not touched"`)
		assert.StrContains(t, p, "contradict the actual code")
	})
	t.Run("no date or time", func(t *testing.T) {
		p := basePrompt(Options{Workspace: "/w"}, "")
		assert.False(t, regexp.MustCompile(`\d{4}-\d{2}-\d{2}|\d{2}:\d{2}`).MatchString(p))
	})
	t.Run("skills listing comes before the environment", func(t *testing.T) {
		p := basePrompt(Options{Workspace: "/w"}, "# Skills\n- a — b")
		assert.True(t, strings.Index(p, "# Skills\n- a — b") < strings.Index(p, "# Environment"))
	})
	t.Run("stable prefix is shared across workspaces and modes", func(t *testing.T) {
		// Providers cache by prefix: everything before the environment must
		// not depend on the workspace or the write mode.
		skills := "# Skills\n- a — b"
		a := basePrompt(Options{Workspace: "/one"}, skills)
		b := basePrompt(Options{Workspace: "/two/longer", AllowWrite: true}, skills)
		cut := strings.Index(a, "# Environment")
		assert.True(t, cut > 0)
		assert.Eq(t, a[:cut], b[:cut])
		assert.StrNotContains(t, a[:cut], "/one")
	})
}

// TestSystemPromptIsSentAndStable runs two turns of one session and checks the
// system message carries the base prompt with identical bytes both times.
func TestSystemPromptIsSentAndStable(t *testing.T) {
	resetConfig(t)
	var mu sync.Mutex
	var systems []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		for _, m := range body.Messages {
			if m.Role == "system" {
				text, _ := json.Marshal(m.Content)
				systems = append(systems, string(text))
			}
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(strings.Replace(okBody, "%s", "x", 1)))
	}))
	t.Cleanup(srv.Close)
	setProviders(t, map[string]any{"openai": map[string]any{"api_key": "k", "base_url": srv.URL}})
	assert.NoErr(t, gkconfig.Set("model", "openai/gpt-4o"))

	rt := openTestRuntime(t, "")
	first, err := rt.Runner.Run(context.Background(), personal.RunRequest{Prompt: "hi"})
	assert.Require(t, assert.NoErr(t, err))
	_, err = rt.Runner.Run(context.Background(), personal.RunRequest{SessionID: first.SessionID, Prompt: "again"})
	assert.Require(t, assert.NoErr(t, err))

	assert.Require(t, assert.Len(t, systems, 2))
	assert.StrContains(t, systems[0], "You are suag")
	assert.StrContains(t, systems[0], "read_only 模式")
	assert.Eq(t, systems[0], systems[1])
}
