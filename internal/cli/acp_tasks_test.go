package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"

	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/personal/store"
)

// allowingACPClient allows every permission request.
type allowingACPClient struct {
	appACPClient
}

func (*allowingACPClient) RequestPermission(context.Context, acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	return acp.RequestPermissionResponse{Outcome: acp.RequestPermissionOutcome{
		Selected: &acp.RequestPermissionOutcomeSelected{OptionId: "allow"}}}, nil
}

// taskEvents returns the session's task events.
func taskEvents(t *testing.T, sessionID string) []store.Event {
	t.Helper()
	dbPath, err := appconfig.ResolveCoreStorePath()
	assert.Require(t, assert.NoErr(t, err))
	repo, err := store.Open(dbPath)
	assert.Require(t, assert.NoErr(t, err))
	defer func() { _ = repo.Close() }()
	events, err := repo.ListEvents(context.Background(), store.EventFilter{SessionID: sessionID})
	assert.Require(t, assert.NoErr(t, err))
	var out []store.Event
	for _, e := range events {
		if e.Kind == store.EventTask {
			out = append(out, e)
		}
	}
	return out
}

// TestAppACPBackgroundTasks: a task outlives the prompt that started it (each
// prompt opens its own runtime), its end is recorded while no turn runs, the
// next prompt gets the notice, and a task still running when the connection
// closes is killed and recorded.
func TestAppACPBackgroundTasks(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	t.Setenv("SUAG_HOME", shortCLIHome(t))
	cwd := t.TempDir()
	quick, slow := "echo TASK_DONE", "sleep 30"
	if runtime.GOOS == "windows" {
		quick, slow = "Write-Output TASK_DONE", "Start-Sleep -Seconds 30"
	}

	// The fake model starts a background task for a "start ..." prompt, reports
	// whether a "check" prompt's request carries the notice, and otherwise
	// answers at once.
	var mu sync.Mutex
	noticeSeen := ""
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		assert.NoErr(t, json.NewDecoder(r.Body).Decode(&body))
		// The notice is appended after the prompt; the prompt is the last user
		// message that is not a notice.
		// Anything else after it (the started task's tool call) means: answer.
		var last struct{ Role, Content string }
		for i := len(body.Messages) - 1; i >= 0; i-- {
			m := body.Messages[i]
			if m.Role == "user" && strings.HasPrefix(m.Content, "[background task notice]") {
				continue
			}
			if m.Role == "user" {
				last.Role, last.Content = m.Role, m.Content
			}
			break
		}
		w.Header().Set("Content-Type", "application/json")
		usage := `"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}`
		command := ""
		switch {
		case last.Role == "user" && last.Content == "start quick":
			command = quick
		case last.Role == "user" && last.Content == "start slow":
			command = slow
		case last.Role == "user" && last.Content == "check":
			for _, m := range body.Messages {
				if strings.Contains(m.Content, "[background task notice]") {
					mu.Lock()
					noticeSeen = m.Content
					mu.Unlock()
				}
			}
		}
		if command != "" {
			args, _ := json.Marshal(map[string]any{"command": command, "background": true})
			call, _ := json.Marshal(map[string]any{"id": "c1", "type": "function",
				"function": map[string]any{"name": "shell_exec", "arguments": string(args)}})
			_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[%s]},"finish_reason":"tool_calls"}],%s}`, call, usage)
			return
		}
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],%s}`, usage)
	}))
	t.Cleanup(provider.Close)
	assert.NoErr(t, gkconfig.Set("model", "openai/gpt-4o"))
	setTestProvider(t, provider.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var sessionID acp.SessionId
	// Cleanups run last-in first-out: this check runs after the connection
	// registered below is closed (and the ACP server has returned).
	t.Cleanup(func() {
		events := taskEvents(t, string(sessionID))
		assert.Require(t, assert.Len(t, events, 2))
		assert.Eq(t, "t2", events[1].Name)
		assert.Eq(t, "killed", events[1].Status)
	})
	client := &allowingACPClient{}
	conn := connectAppACPWith(t, client)
	_, err := conn.Initialize(ctx, acp.InitializeRequest{ProtocolVersion: acp.ProtocolVersionNumber})
	assert.Require(t, assert.NoErr(t, err))
	created, err := conn.NewSession(ctx, acp.NewSessionRequest{Cwd: cwd, McpServers: []acp.McpServer{}})
	assert.Require(t, assert.NoErr(t, err))
	sessionID = created.SessionId
	prompt := func(text string) {
		t.Helper()
		res, err := conn.Prompt(ctx, acp.PromptRequest{SessionId: sessionID, Prompt: []acp.ContentBlock{acp.TextBlock(text)}})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, acp.StopReasonEndTurn, res.StopReason)
	}

	t.Run("a task ending between turns is recorded", func(t *testing.T) {
		prompt("start quick")
		deadline := time.Now().Add(10 * time.Second)
		for len(taskEvents(t, string(sessionID))) == 0 && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		events := taskEvents(t, string(sessionID))
		assert.Require(t, assert.Len(t, events, 1))
		assert.Eq(t, "t1", events[0].Name)
		assert.Eq(t, "ok", events[0].Status)
	})
	t.Run("the next prompt gets the notice", func(t *testing.T) {
		prompt("check")
		mu.Lock()
		defer mu.Unlock()
		assert.StrContains(t, noticeSeen, "t1 exited (exit 0)")
		assert.StrContains(t, noticeSeen, "TASK_DONE")
	})
	t.Run("a task outlives its prompt", func(t *testing.T) {
		prompt("start slow")
		prompt("check") // a later turn opens a new runtime; t2 keeps running
		assert.Len(t, taskEvents(t, string(sessionID)), 1)
	})
}
