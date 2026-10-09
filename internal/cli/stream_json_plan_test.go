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

// TestStreamJSONPlanEvent: a plan line appears only when the model calls
// update_plan, with the full replaced list; it works under --read-only.
func TestStreamJSONPlanEvent(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	t.Setenv("SUAG_HOME", shortTestHome(t))
	workspace := t.TempDir()
	t.Chdir(workspace)
	assert.NoErr(t, gkconfig.Set("workspace", workspace))
	assert.NoErr(t, gkconfig.Set("model", "openai/gpt-4o"))

	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.Header().Set("Content-Type", "application/json")
		if count == 1 {
			args := `{\"explanation\":\"go\",\"plan\":[{\"step\":\"A\",\"status\":\"in_progress\"},{\"step\":\"B\",\"status\":\"pending\"}]}`
			_, _ = io.WriteString(w, `{"choices":[{"message":{"tool_calls":[{"id":"call-1","function":{"name":"update_plan","arguments":"`+args+`"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"- [x] not a plan"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
	}))
	t.Cleanup(server.Close)
	setTestProvider(t, server.URL)

	var out, diag bytes.Buffer
	args := []string{"--prompt", "plan", "--output-format", "stream-json", "--read-only"}
	assert.NoErr(t, RunPersonalCLI(context.Background(), "run", args, strings.NewReader(""), &out, &diag))

	var plans []map[string]any
	for _, raw := range bytes.Split(out.Bytes(), []byte("\n")) {
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var line map[string]any
		assert.NoErr(t, json.Unmarshal(raw, &line))
		if line["type"] == "plan" {
			plans = append(plans, line)
		}
	}
	assert.Require(t, assert.Eq(t, 1, len(plans)))
	plan := plans[0]
	assert.Eq(t, true, plan["session_id"] != "" && plan["run_id"] != "")
	assert.Eq(t, "go", plan["explanation"])
	assert.Eq(t, []any{
		map[string]any{"step": "A", "status": "in_progress"},
		map[string]any{"step": "B", "status": "pending"},
	}, plan["entries"])
}
