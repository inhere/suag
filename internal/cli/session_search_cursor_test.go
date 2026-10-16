package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore"
)

// TestSessionSearchProviderCursorChain drives the real CLI, Ledger and native
// tool against a fake OpenAI provider: the model receives next_cursor and sends
// that exact token back on its second session_search call.
func TestSessionSearchProviderCursorChain(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	home := shortTestHome(t)
	t.Setenv("SUGA_HOME", home)
	workspace := t.TempDir()
	t.Chdir(workspace)
	assert.NoError(t, gkconfig.Set("workspace", workspace))
	assert.NoError(t, gkconfig.Set("model", "openai/gpt-4o"))

	repo, err := store.Open(filepath.Join(home, "db", "sessions.db"))
	assert.Require(t, assert.NoError(t, err))
	ctx := context.Background()
	assert.NoError(t, repo.CreateSession(ctx, store.Session{ID: "search-chain", Cwd: workspace}))
	for _, content := range []string{"needle first", "needle second"} {
		body, marshalErr := json.Marshal(agentcore.Message{Role: "user", Content: content})
		assert.Require(t, assert.NoError(t, marshalErr))
		_, appendErr := repo.AppendEntries(ctx, "search-chain", store.NewEntry{Type: store.EntryUser, JSON: body})
		assert.Require(t, assert.NoError(t, appendErr))
	}
	assert.NoError(t, repo.Close())

	var calls int
	var firstResultCursor, secondQueryCursor string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		var toolResults []string
		for _, message := range request.Messages {
			if message.Role == "tool" {
				toolResults = append(toolResults, message.Content)
			}
		}
		if calls == 2 {
			if len(toolResults) == 0 {
				t.Fatal("second provider request did not include the first tool result")
			}
			last := toolResults[len(toolResults)-1]
			const marker = "next_cursor:"
			at := strings.Index(last, marker)
			if at >= 0 {
				fields := strings.Fields(last[at+len(marker):])
				if len(fields) > 0 {
					firstResultCursor = fields[0]
				}
			}
		}
		if calls == 3 {
			if len(toolResults) == 0 {
				t.Fatal("third provider request did not include the second tool result")
			}
			assert.NotContains(t, toolResults[len(toolResults)-1], "next_cursor:")
			assert.StrContains(t, toolResults[len(toolResults)-1], "search-chain/2")
		}

		w.Header().Set("Content-Type", "application/json")
		if calls <= 2 {
			args := map[string]any{"query": "needle", "scope": "all", "limit": 1}
			if calls == 2 {
				args["cursor"] = firstResultCursor
				secondQueryCursor = firstResultCursor
			}
			encoded, marshalErr := json.Marshal(args)
			assert.NoError(t, marshalErr)
			response := map[string]any{
				"choices": []any{map[string]any{
					"message": map[string]any{"tool_calls": []any{map[string]any{
						"id": fmt.Sprintf("search-%d", calls), "type": "function",
						"function": map[string]any{"name": "session_search", "arguments": string(encoded)},
					}}},
					"finish_reason": "tool_calls",
				}},
				"usage": map[string]any{"prompt_tokens": 2, "completion_tokens": 1, "total_tokens": 3},
			}
			assert.NoError(t, json.NewEncoder(w).Encode(response))
			return
		}
		response := map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"content": "cursor chain complete"}, "finish_reason": "stop",
			}},
		}
		assert.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	t.Cleanup(server.Close)
	setTestProvider(t, server.URL)

	var output, diagnostics bytes.Buffer
	err = RunPersonalCLI(ctx, "run", []string{
		"--prompt", "search now", "--resume", "search-chain", "--output-format", "stream-json", "--read-only",
	}, strings.NewReader(""), &output, &diagnostics)
	assert.NoError(t, err)
	assert.Eq(t, 3, calls)
	assert.NotEmpty(t, firstResultCursor)
	assert.Eq(t, firstResultCursor, secondQueryCursor)
	assert.StrContains(t, output.String(), "cursor chain complete")
	assert.Empty(t, diagnostics.String())
}
