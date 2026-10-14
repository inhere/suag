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

	"github.com/inhere/suga/internal/personal"
)

func TestBasePromptDebugNonce(t *testing.T) {
	t.Run("unset has no nonce", func(t *testing.T) {
		t.Setenv(debugPromptNonceEnv, "")
		p := basePrompt()
		assert.Eq(t, false, strings.Contains(p, "nonce:"))
		assert.Eq(t, true, strings.HasPrefix(p, "You are suga"))
	})
	t.Run("set goes first", func(t *testing.T) {
		t.Setenv(debugPromptNonceEnv, "abc123")
		p := basePrompt()
		assert.Eq(t, true, strings.HasPrefix(p, "nonce: abc123\nYou are suga"))
	})
}

func TestBasePrompt(t *testing.T) {
	t.Run("identity and conventions only", func(t *testing.T) {
		p := basePrompt()
		assert.StrContains(t, p, `"Found, not touched"`)
		assert.StrContains(t, p, "contradict the actual code")
		// Workspace, mode and skills live in the segment context (design D1).
		assert.NotContains(t, p, "# Environment")
		assert.NotContains(t, p, "Mode:")
		assert.NotContains(t, p, "Workspace")
		assert.NotContains(t, p, "# Skills")
	})
	t.Run("build and test output is left to the minimizer", func(t *testing.T) {
		p := basePrompt()
		assert.StrContains(t, p, "is minimized into a summary plus the failure details")
		assert.StrContains(t, p, "`| Select-Object -Last`")
		assert.StrContains(t, p, "do not pass `-q` to mvn or gradle")
	})
	t.Run("dependency APIs are named per language", func(t *testing.T) {
		p := basePrompt()
		assert.StrContains(t, p, "`go doc <package>`")
		assert.StrContains(t, p, "`javap -public -cp <jar> <fully.qualified.Class>`")
		assert.StrContains(t, p, "~/.m2/repository")
	})
	t.Run("no date or time", func(t *testing.T) {
		re := regexp.MustCompile(`\d{4}-\d{2}-\d{2}|\d{2}:\d{2}`)
		assert.False(t, re.MatchString(basePrompt()))
		assert.False(t, re.MatchString(environmentPrompt(Options{Workspace: "/w"})))
	})
}

func TestEnvironmentPrompt(t *testing.T) {
	t.Run("read-only names the enable hint for the entry", func(t *testing.T) {
		p := environmentPrompt(Options{Workspace: "/w", CLIMode: true})
		assert.True(t, strings.HasPrefix(p, "# Environment\n"))
		assert.StrContains(t, p, "Mode: read-only")
		assert.StrContains(t, p, "加 --allow-write")
		assert.StrContains(t, p, "Workspace (file tools are confined to it): /w")
		assert.NotContains(t, p, "Shell for shell_exec")
	})
	t.Run("read roots are listed", func(t *testing.T) {
		resetConfig(t)
		dir := t.TempDir()
		assert.NoErr(t, gkconfig.Set("tools.dependency_roots", false))
		assert.NoErr(t, gkconfig.Set("tools.read_roots", []string{dir}))
		p := environmentPrompt(Options{Workspace: "/w"})
		assert.StrContains(t, p, "Read-only roots outside the workspace")
		assert.StrContains(t, p, "  - configured: "+dir)
		assert.Eq(t, []string{dir}, nativeEnv("/w", nil).ReadRoots)
	})
	t.Run("no read roots, no section", func(t *testing.T) {
		resetConfig(t)
		assert.NoErr(t, gkconfig.Set("tools.dependency_roots", false))
		assert.NotContains(t, environmentPrompt(Options{Workspace: "/w"}), "Read-only roots")
		assert.Empty(t, nativeEnv("/w", nil).ReadRoots)
	})
	t.Run("writable names the shell", func(t *testing.T) {
		p := environmentPrompt(Options{Workspace: "/w", AllowWrite: true})
		assert.StrContains(t, p, "Mode: writable")
		assert.StrContains(t, p, "Shell for shell_exec: ")
	})
}

// requestRecorder is an OpenAI-compatible fake that keeps every request's
// system message and the first non-system message.
type requestRecorder struct {
	mu       sync.Mutex
	systems  []string
	firsts   []string
	rawFirst []map[string]any
}

func (rr *requestRecorder) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Messages []map[string]any `json:"messages"`
		}
		_ = json.Unmarshal(raw, &body)
		rr.mu.Lock()
		for i, m := range body.Messages {
			text, _ := m["content"].(string)
			if m["role"] == "system" {
				rr.systems = append(rr.systems, text)
				continue
			}
			if i <= 1 {
				rr.firsts = append(rr.firsts, text)
				rr.rawFirst = append(rr.rawFirst, m)
			}
		}
		rr.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(strings.Replace(okBody, "%s", "x", 1)))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestSystemPromptIsSentAndStable runs two turns of one session and checks the
// system message carries the base prompt with identical bytes both times,
// while the environment travels in the segment context message.
func TestSystemPromptIsSentAndStable(t *testing.T) {
	resetConfig(t)
	rr := &requestRecorder{}
	srv := rr.server(t)
	setProviders(t, map[string]any{"openai": map[string]any{"api_key": "k", "base_url": srv.URL}})
	assert.NoErr(t, gkconfig.Set("model", "openai/gpt-4o"))

	rt := openTestRuntime(t, "")
	first, err := rt.Runner.Run(context.Background(), personal.RunRequest{Prompt: "hi"})
	assert.Require(t, assert.NoErr(t, err))
	_, err = rt.Runner.Run(context.Background(), personal.RunRequest{SessionID: first.SessionID, Prompt: "again"})
	assert.Require(t, assert.NoErr(t, err))

	assert.Require(t, assert.Len(t, rr.systems, 2))
	assert.Eq(t, basePrompt(), rr.systems[0])
	assert.Eq(t, rr.systems[0], rr.systems[1])
	assert.Require(t, assert.Len(t, rr.firsts, 2))
	// The read-only hint is in the segment context, not the system prompt.
	assert.StrContains(t, rr.firsts[0], "# Environment")
	assert.StrContains(t, rr.firsts[0], "read_only 模式")
	assert.Eq(t, rr.firsts[0], rr.firsts[1])
	// The segment context marker never reaches the wire.
	_, leaked := rr.rawFirst[0]["metadata"]
	assert.False(t, leaked)
}

// TestSystemPromptSharedAcrossWorkspacesAndModes: sessions in two different
// workspaces (same mode), and in another mode, send byte-identical system
// prompts; only the segment context differs (design D1).
func TestSystemPromptSharedAcrossWorkspacesAndModes(t *testing.T) {
	resetConfig(t)
	rr := &requestRecorder{}
	srv := rr.server(t)
	setProviders(t, map[string]any{"openai": map[string]any{"api_key": "k", "base_url": srv.URL}})
	assert.NoErr(t, gkconfig.Set("model", "openai/gpt-4o"))

	run := func(opts Options) {
		rt, err := Open(context.Background(), opts)
		assert.Require(t, assert.NoErr(t, err))
		defer func() { _ = rt.Close() }()
		_, err = rt.Runner.Run(context.Background(), personal.RunRequest{Prompt: "hi"})
		assert.Require(t, assert.NoErr(t, err))
	}
	one, two := t.TempDir(), t.TempDir()
	run(Options{Workspace: one, RuleCWD: one})
	run(Options{Workspace: two, RuleCWD: two})
	run(Options{Workspace: two, RuleCWD: two, AllowWrite: true})

	assert.Require(t, assert.Len(t, rr.systems, 3))
	assert.Eq(t, rr.systems[0], rr.systems[1])
	assert.Eq(t, rr.systems[0], rr.systems[2])
	assert.NotContains(t, rr.systems[0], one)
	assert.Require(t, assert.Len(t, rr.firsts, 3))
	assert.StrContains(t, rr.firsts[0], one)
	assert.StrContains(t, rr.firsts[1], two)
	assert.StrContains(t, rr.firsts[2], "Mode: writable")
}
