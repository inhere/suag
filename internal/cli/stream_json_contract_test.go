package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
)

// contractLine is the shape of one kind of stream-json line: its type, optional
// status and the sorted set of field names. Values (run_id, text, usage) are
// deliberately excluded so the contract survives id/format changes.
type contractLine struct {
	Type   string   `json:"type"`
	Status string   `json:"status,omitempty"`
	Fields []string `json:"fields"`
}

// goldenEnv, when set to a path, makes TestStreamJSONContract (re)write the golden
// file there instead of comparing; used once against the pre-store baseline.
const goldenEnv = "SUAG_WRITE_STREAM_GOLDEN"

// runContractScenario drives RunPersonalCLI("run") against a fake OpenAI server and
// returns the distinct stream-json line shapes it emitted.
func runContractScenario(t *testing.T, scenario string) []contractLine {
	t.Helper()
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	t.Setenv("SUAG_HOME", shortTestHome(t))
	workspace := t.TempDir()
	t.Chdir(workspace)
	assert.NoErr(t, gkconfig.Set("workspace", workspace))
	assert.NoErr(t, gkconfig.Set("llm.api_type", "openai"))
	assert.NoErr(t, gkconfig.Set("llm.api_key", "test-key"))

	args := []string{"--prompt", "contract", "--output-format", "stream-json"}
	var count int
	handler := func(w http.ResponseWriter, r *http.Request) {
		count++
		w.Header().Set("Content-Type", "application/json")
		switch scenario {
		case "error":
			http.Error(w, `{"error":{"message":"boom"}}`, http.StatusInternalServerError)
		case "input_required":
			if count == 1 {
				_, _ = io.WriteString(w, `{"choices":[{"message":{"tool_calls":[{"id":"call-1","function":{"name":"gofer_issue_close","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
				return
			}
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"model-finished"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
		default:
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(server.Close)
	assert.NoErr(t, gkconfig.Set("llm.base_url", server.URL))

	if scenario == "input_required" {
		// A write MCP tool without --allow-write is denied by the CLI gate.
		mcpConfig := filepath.Join(t.TempDir(), "mcp.json")
		config, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"gofer": map[string]any{
			"command": os.Args[0], "args": []string{"-test.run=TestFakeMCPServerProcess"},
			"env": map[string]string{"SUAG_FAKE_MCP": "1"},
		}}})
		assert.NoErr(t, err)
		assert.NoErr(t, os.WriteFile(mcpConfig, config, 0600))
		args = append(args, "--mcp-config", mcpConfig)
	}

	var out, diag bytes.Buffer
	err := RunPersonalCLI(context.Background(), "run", args, strings.NewReader(""), &out, &diag)
	if scenario == "error" {
		assert.True(t, err != nil)
	} else {
		assert.NoErr(t, err)
	}
	return shapesOf(t, out.Bytes())
}

// shapesOf reduces stream-json output to the sorted, de-duplicated line shapes.
func shapesOf(t *testing.T, out []byte) []contractLine {
	t.Helper()
	seen := map[string]contractLine{}
	for _, raw := range bytes.Split(out, []byte("\n")) {
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var line map[string]any
		assert.NoErr(t, json.Unmarshal(raw, &line))
		shape := contractLine{Fields: []string{}}
		shape.Type, _ = line["type"].(string)
		shape.Status, _ = line["status"].(string)
		for k := range line {
			shape.Fields = append(shape.Fields, k)
		}
		sort.Strings(shape.Fields)
		seen[fmt.Sprintf("%s|%s|%v", shape.Type, shape.Status, shape.Fields)] = shape
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	shapes := make([]contractLine, 0, len(keys))
	for _, k := range keys {
		shapes = append(shapes, seen[k])
	}
	return shapes
}

// TestStreamJSONContract pins the field sets and type/status values of the
// session, result (completed), input_required and error stream-json lines to the
// sample recorded before the single-store switch.
func TestStreamJSONContract(t *testing.T) {
	got := map[string][]contractLine{}
	for _, scenario := range []string{"result", "input_required", "error"} {
		scenario := scenario
		t.Run(scenario, func(t *testing.T) {
			got[scenario] = runContractScenario(t, scenario)
		})
	}
	if path := os.Getenv(goldenEnv); path != "" {
		data, err := json.MarshalIndent(got, "", "  ")
		assert.NoErr(t, err)
		assert.NoErr(t, os.WriteFile(path, append(data, '\n'), 0o644))
		return
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "stream_json_contract.golden.json"))
	assert.NoErr(t, err)
	var want map[string][]contractLine
	assert.NoErr(t, json.Unmarshal(raw, &want))
	for _, scenario := range []string{"result", "input_required", "error"} {
		t.Run("golden/"+scenario, func(t *testing.T) {
			assert.Eq(t, want[scenario], got[scenario])
		})
	}
}
