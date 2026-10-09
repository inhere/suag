package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
)

// TestWiringOptionsChatMode checks that only chat opens the runtime in chat
// mode, where tools.allow_write and the chat approver apply.
func TestWiringOptionsChatMode(t *testing.T) {
	opts := personalOptions{AllowWrite: true, ReadOnly: true}
	t.Run("chat", func(t *testing.T) {
		wopts := opts.wiringOptions("chat")
		assert.True(t, wopts.ChatMode)
		assert.True(t, wopts.AllowWrite)
		assert.True(t, wopts.ReadOnly)
	})
	t.Run("run", func(t *testing.T) {
		assert.False(t, opts.wiringOptions("run").ChatMode)
	})
}

// TestChatApprovalWithoutTerminal runs chat on a non-terminal stdin with
// tools.allow_write: the asker is never bound, so shell_exec is refused with a
// hint under tools.approval ask (default) and runs under auto.
func TestChatApprovalWithoutTerminal(t *testing.T) {
	for _, tc := range []struct {
		name, approval string
		runs           bool
	}{
		{name: "ask refuses", approval: ""},
		{name: "explicit ask refuses", approval: "ask"},
		{name: "auto runs without asking", approval: "auto", runs: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gkconfig.Reset()
			t.Cleanup(gkconfig.Reset)
			t.Setenv("SUAG_HOME", shortTestHome(t))
			workspace := t.TempDir()
			t.Chdir(workspace)
			assert.NoErr(t, gkconfig.Set("workspace", workspace))
			assert.NoErr(t, gkconfig.Set("model", "openai/gpt-4o"))
			assert.NoErr(t, gkconfig.Set("tools.allow_write", true))
			if tc.approval != "" {
				assert.NoErr(t, gkconfig.Set("tools.approval", tc.approval))
			}
			var toolResult string
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				if calls == 1 {
					_, _ = io.WriteString(w, `{"choices":[{"message":{"tool_calls":[{"id":"call-1","function":{"name":"shell_exec","arguments":"{\"command\":\"echo SMOKE_OK\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
					return
				}
				var body struct {
					Messages []struct{ Role, Content string } `json:"messages"`
				}
				assert.NoErr(t, json.NewDecoder(r.Body).Decode(&body))
				for _, message := range body.Messages {
					if message.Role == "tool" {
						toolResult = message.Content
					}
				}
				_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"model-finished"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
			}))
			t.Cleanup(server.Close)
			setTestProvider(t, server.URL)

			var output, diagnostics bytes.Buffer
			err := RunPersonalCLI(context.Background(), "chat", []string{}, strings.NewReader("run it\n:quit\n"), &output, &diagnostics)
			assert.NoErr(t, err)
			assert.Eq(t, 2, calls)
			if tc.runs {
				assert.StrContains(t, toolResult, "SMOKE_OK")
				assert.NotContains(t, output.String(), "本轮被拒绝的工具")
				return
			}
			assert.NotContains(t, toolResult, "SMOKE_OK")
			assert.StrContains(t, toolResult, "tools.approval: auto")
			assert.StrContains(t, output.String(), "本轮被拒绝的工具：shell_exec")
		})
	}
}
