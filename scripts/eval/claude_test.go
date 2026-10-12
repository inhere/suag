package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestParseClaudeEvents(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"s1","tools":["Read","Edit"]}`,
		// one API response split over two assistant events with the same id
		`{"type":"assistant","message":{"id":"m1","content":[{"type":"thinking","thinking":"..."}],"usage":{"input_tokens":1000,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":5}}}`,
		`{"type":"assistant","message":{"id":"m1","content":[{"type":"tool_use","name":"Read"},{"type":"tool_use","name":"Grep"}],"usage":{"input_tokens":1000,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":40}}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1"}]}}`,
		`{"type":"assistant","message":{"id":"m2","content":[{"type":"tool_use","name":"Edit"}],"usage":{"input_tokens":100,"cache_read_input_tokens":900,"cache_creation_input_tokens":50,"output_tokens":30}}}`,
		`{"type":"assistant","message":{"id":"m3","content":[{"type":"text","text":"done"}],"usage":{"input_tokens":20,"cache_read_input_tokens":1000,"output_tokens":10}}}`,
		`{"type":"result","subtype":"success","result":"done, all tests pass","num_turns":3,"total_cost_usd":0.01}`,
	}, "\n")
	var r Result
	parseClaudeEvents([]byte(stream), &r)
	assert.Eq(t, 3, r.Calls)
	assert.Eq(t, 3, r.ToolCalls)
	assert.Eq(t, map[string]int{"Read": 1, "Grep": 1, "Edit": 1}, r.ToolCounts)
	assert.Eq(t, int64(1120), r.InputTokens) // m1 counted once, with its last usage
	assert.Eq(t, int64(80), r.OutputToks)
	assert.Eq(t, int64(1900), r.CacheRead)
	assert.Eq(t, int64(50), r.CacheWrite)
	assert.Eq(t, int64(1000), *r.FirstUncached)
	assert.Eq(t, "done, all tests pass", r.Answer)
	assert.Eq(t, 0.01, r.CostUSD)
}

// TestParseClaudeEventsResultUsage: through a gateway the per-message usage
// is zero; the result events of the turns give the totals.
func TestParseClaudeEventsResultUsage(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"assistant","message":{"id":"a","content":[{"type":"tool_use","name":"Read"}],"usage":{"input_tokens":0,"output_tokens":0}}}`,
		`{"type":"assistant","message":{"id":"b","content":[{"type":"text","text":"x"}],"usage":{"input_tokens":0,"output_tokens":0}}}`,
		`{"type":"result","result":"first","usage":{"input_tokens":100,"cache_read_input_tokens":300,"output_tokens":10}}`,
		`{"type":"assistant","message":{"id":"c","content":[{"type":"text","text":"y"}],"usage":{"input_tokens":0,"output_tokens":0}}}`,
		`{"type":"result","result":"second","usage":{"input_tokens":1,"output_tokens":1},"modelUsage":{"m":{"inputTokens":30,"cacheReadInputTokens":300,"outputTokens":3},"sub":{"inputTokens":20,"cacheReadInputTokens":100,"outputTokens":2}}}`,
	}, "\n")
	var r Result
	parseClaudeEvents([]byte(stream), &r)
	assert.Eq(t, 3, r.Calls)
	assert.Eq(t, int64(150), r.InputTokens)
	assert.Eq(t, int64(700), r.CacheRead)
	assert.Eq(t, int64(15), r.OutputToks)
	assert.Nil(t, r.FirstUncached) // unknown
	assert.Eq(t, "second", r.Answer)
}

func TestWriteClaudeSettings(t *testing.T) {
	from, dir := t.TempDir(), filepath.Join(t.TempDir(), "claude-home")
	assert.NoErr(t, os.WriteFile(filepath.Join(from, "settings.json"), []byte(`{
		"env": {"ANTHROPIC_BASE_URL": "http://127.0.0.1:1", "ANTHROPIC_AUTH_TOKEN": "tok"},
		"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "gofer hook claude"}]}]},
		"statusLine": {"type": "command", "command": "x"}}`), 0o644))
	assert.NoErr(t, writeClaudeSettings(from, dir))
	raw, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	assert.Require(t, assert.NoErr(t, err))
	var got map[string]any
	assert.NoErr(t, json.Unmarshal(raw, &got))
	assert.Eq(t, map[string]any{"ANTHROPIC_BASE_URL": "http://127.0.0.1:1", "ANTHROPIC_AUTH_TOKEN": "tok"}, got["env"])
	assert.Eq(t, true, got["disableAllHooks"])
	assert.Nil(t, got["hooks"])
	assert.Nil(t, got["statusLine"])

	assert.NoErr(t, os.WriteFile(filepath.Join(from, "settings.json"), []byte(`{"hooks": {}}`), 0o644))
	assert.ErrMsg(t, writeClaudeSettings(from, dir), "--claude-config-from settings.json has no env (gateway URL and token)")
}

func TestAncestorInstructions(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "a", "b", "ws")
	assert.NoErr(t, os.MkdirAll(ws, 0o755))
	assert.NoErr(t, os.WriteFile(filepath.Join(ws, "CLAUDE.md"), []byte("own"), 0o644)) // the workspace's own is fine
	assert.Eq(t, "", ancestorInstructions(ws))
	assert.NoErr(t, os.WriteFile(filepath.Join(root, "a", "AGENTS.md"), []byte("x"), 0o644))
	assert.Eq(t, filepath.Join(root, "a", "AGENTS.md"), ancestorInstructions(ws))
}

func TestWriteOutGoMod(t *testing.T) {
	out := t.TempDir()
	assert.NoErr(t, writeOutGoMod(out))
	raw, err := os.ReadFile(filepath.Join(out, "go.mod"))
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, "module evalout\n", string(raw))
	assert.NoErr(t, os.WriteFile(filepath.Join(out, "go.mod"), []byte("module kept\n"), 0o644))
	assert.NoErr(t, writeOutGoMod(out)) // an existing one is kept
	raw, _ = os.ReadFile(filepath.Join(out, "go.mod"))
	assert.Eq(t, "module kept\n", string(raw))
}
