package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/pkg/agentcore"
)

func TestFakeMCPServerProcess(t *testing.T) {
	if os.Getenv("SUAG_FAKE_MCP") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(2)
		}
		var result any = map[string]any{}
		switch request.Method {
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{
				{"name": "gofer_issue_list", "description": "list issues", "inputSchema": map[string]any{"type": "object"}},
				{"name": "gofer_issue_close", "description": "close an issue", "inputSchema": map[string]any{"type": "object"}},
			}}
		case "tools/call":
			result = map[string]any{"content": []map[string]string{{"type": "text", "text": "EXECUTED:" + request.Params.Name}}}
		}
		response, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		fmt.Fprintln(os.Stdout, string(response))
	}
	os.Exit(0)
}

func TestPersonalRunWithStdioMCPReadAndDeniedWrite(t *testing.T) {
	for _, tc := range []struct {
		name, tool string
		mode       string
		allowWrite bool
		readOnly   bool
		defaultMCP bool
		denied     bool
		repeat     bool
	}{
		{name: "read tool executes", tool: "gofer_issue_list", readOnly: true},
		{name: "write stays denied with allow-write", tool: "gofer_issue_close", allowWrite: true, denied: true},
		{name: "chat uses MCP", tool: "gofer_issue_list", mode: "chat"},
		{name: "default MCP config", tool: "gofer_issue_list", defaultMCP: true},
		{name: "repeated write request ends turn", tool: "gofer_issue_close", denied: true, repeat: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gkconfig.Reset()
			t.Cleanup(gkconfig.Reset)
			t.Setenv("DATABASE_URL", "")
			t.Setenv("SUAG_HOME", shortTestHome(t))
			workspace := t.TempDir()
			t.Chdir(workspace)
			assert.NoErr(t, gkconfig.Set("workspace", workspace))
			assert.NoErr(t, gkconfig.Set("llm.api_type", "openai"))
			assert.NoErr(t, gkconfig.Set("llm.api_key", "test-key"))
			mcpConfig := filepath.Join(t.TempDir(), "mcp.json")
			config, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"gofer": map[string]any{
				"command": os.Args[0], "args": []string{"-test.run=TestFakeMCPServerProcess"},
				"env": map[string]string{"SUAG_FAKE_MCP": "1"},
			}}})
			assert.NoErr(t, err)
			assert.NoErr(t, os.WriteFile(mcpConfig, config, 0600))
			extraConfig := filepath.Join(t.TempDir(), "extra.json")
			assert.NoErr(t, os.WriteFile(extraConfig, []byte(`{"mcpServers":{}}`), 0600))
			if tc.defaultMCP {
				assert.NoErr(t, gkconfig.Set("mcp.servers", []map[string]any{{
					"id": "gofer", "command": os.Args[0], "args": []string{"-test.run=TestFakeMCPServerProcess"},
					"env": map[string]any{"SUAG_FAKE_MCP": "1"},
				}}))
			}
			var count int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count++
				w.Header().Set("Content-Type", "application/json")
				if count == 1 || tc.repeat {
					_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"tool_calls":[{"id":"call-1","function":{"name":%q,"arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`, tc.tool)
					return
				}
				var body struct {
					Messages []struct {
						Role, Content string
					} `json:"messages"`
				}
				assert.NoErr(t, json.NewDecoder(r.Body).Decode(&body))
				var toolResult string
				for _, message := range body.Messages {
					if message.Role == "tool" {
						toolResult = message.Content
					}
				}
				if tc.denied {
					assert.Eq(t, true, strings.Contains(toolResult, "需要许可："+tc.tool))
					assert.Eq(t, false, strings.Contains(toolResult, "EXECUTED"))
				} else {
					assert.Eq(t, "EXECUTED:"+tc.tool, toolResult)
				}
				_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"model-finished"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
			}))
			t.Cleanup(server.Close)
			assert.NoErr(t, gkconfig.Set("llm.base_url", server.URL))
			mode := tc.mode
			if mode == "" {
				mode = "run"
			}
			args := []string{}
			input := ""
			if mode == "run" {
				args = append(args, "--prompt", "test MCP", "--output-format", "stream-json")
			} else {
				input = "test MCP\n:quit\n"
			}
			if !tc.defaultMCP {
				args = append(args, "--mcp-config", mcpConfig, "--mcp-config", extraConfig)
			}
			if tc.allowWrite {
				args = append(args, "--allow-write")
			}
			if tc.readOnly {
				args = append(args, "--read-only")
			}
			var output, diagnostics bytes.Buffer
			err = RunPersonalCLI(context.Background(), mode, args, strings.NewReader(input), &output, &diagnostics)
			assert.NoErr(t, err)
			assert.Eq(t, 2, count)
			if mode == "run" {
				assert.Eq(t, true, strings.Contains(output.String(), `"input_tokens":4`))
				if !tc.repeat {
					assert.Eq(t, true, strings.Contains(output.String(), `"result":"model-finished"`))
				}
				assert.Eq(t, tc.denied, strings.Contains(output.String(), `"type":"input_required"`))
			} else {
				assert.Eq(t, true, strings.Contains(output.String(), "model-finished"))
			}
		})
	}
}

func TestCLIMCPGateDefaultsAndOverrides(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	registry := tools.NewRegistry()
	for _, name := range []string{"gofer_issue_list", "gofer_work_get", "gofer_work_report", "gofer_issue_close", "mcp_write"} {
		registry.Register(newServerStubTool("gofer", name))
	}
	gate, err := newCLIMCPGate(registry, false)
	assert.NoErr(t, err)
	for _, name := range []string{"gofer_issue_list", "gofer_work_get", "gofer_work_report"} {
		_, err := gate.ExecuteTool(context.Background(), agentcore.ToolCall{Name: name})
		assert.NoErr(t, err)
	}
	for _, name := range []string{"gofer_issue_close", "mcp_write"} {
		_, err := gate.ExecuteTool(context.Background(), agentcore.ToolCall{Name: name})
		assert.True(t, err != nil && strings.Contains(err.Error(), "需要许可："+name))
	}
	assert.Eq(t, true, gate.WasDenied())
	// The refusal matches the denial sentinel the observe layer keys on.
	_, err = gate.ExecuteTool(context.Background(), agentcore.ToolCall{Name: "mcp_write"})
	assert.True(t, errors.Is(err, tools.ErrDenied))
	assert.NoErr(t, gkconfig.Set("tools.auto_allow", []string{"mcp_write"}))
	gate, err = newCLIMCPGate(registry, false)
	assert.NoErr(t, err)
	_, err = gate.ExecuteTool(context.Background(), agentcore.ToolCall{Name: "mcp_write"})
	assert.NoErr(t, err)
	_, err = gate.ExecuteTool(context.Background(), agentcore.ToolCall{Name: "gofer_work_report"})
	assert.True(t, err != nil)
}

// TestCLIMCPGateReadOnly verifies --read-only ignores user auto_allow extensions.
func TestCLIMCPGateReadOnly(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	registry := tools.NewRegistry()
	for _, name := range []string{"gofer_x_list", "gofer_work_report", "mcp_write", "gofer_issue_close"} {
		registry.Register(newServerStubTool("gofer", name))
	}
	assert.NoErr(t, gkconfig.Set("tools.auto_allow", []string{"mcp_write", "gofer_*"}))

	t.Run("read-only denies user-allowed write tools", func(t *testing.T) {
		gate, err := newCLIMCPGate(registry, true)
		assert.NoErr(t, err)
		for _, name := range []string{"mcp_write", "gofer_issue_close"} {
			_, err := gate.ExecuteTool(context.Background(), agentcore.ToolCall{Name: name})
			assert.True(t, err != nil && strings.Contains(err.Error(), "需要许可："+name))
		}
		assert.Eq(t, true, gate.WasDenied())
	})
	t.Run("read-only allows default query tools and work report", func(t *testing.T) {
		gate, err := newCLIMCPGate(registry, true)
		assert.NoErr(t, err)
		for _, name := range []string{"gofer_x_list", "gofer_work_report"} {
			_, err := gate.ExecuteTool(context.Background(), agentcore.ToolCall{Name: name})
			assert.NoErr(t, err)
		}
		assert.Eq(t, false, gate.WasDenied())
	})
	t.Run("non read-only keeps user allowlist", func(t *testing.T) {
		gate, err := newCLIMCPGate(registry, false)
		assert.NoErr(t, err)
		_, err = gate.ExecuteTool(context.Background(), agentcore.ToolCall{Name: "mcp_write"})
		assert.NoErr(t, err)
	})
}

// TestCLIMCPGateServerQualified verifies the default allowlist trusts only the
// server named "gofer", so a third-party server cannot borrow a gofer tool name.
func TestCLIMCPGateServerQualified(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	registry := tools.NewRegistry()
	registry.Register(newServerStubTool("gofer", "gofer_work_report"))
	registry.Register(newServerStubTool("thirdparty", "gofer_session_list"))
	registry.Register(newServerStubTool("thirdparty", "gofer_issue_get"))

	run := func(t *testing.T, readOnly bool, name string) error {
		gate, err := newCLIMCPGate(registry, readOnly)
		assert.NoErr(t, err)
		_, err = gate.ExecuteTool(context.Background(), agentcore.ToolCall{Name: name})
		return err
	}
	for _, readOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("readOnly=%v", readOnly), func(t *testing.T) {
			assert.NoErr(t, run(t, readOnly, "gofer_work_report"))
			for _, name := range []string{"gofer_session_list", "gofer_issue_get"} {
				err := run(t, readOnly, name)
				assert.True(t, err != nil && strings.Contains(err.Error(), "需要许可："+name))
			}
		})
	}
	t.Run("user entries: qualified is server-scoped, bare matches any server", func(t *testing.T) {
		assert.NoErr(t, gkconfig.Set("tools.auto_allow", []string{"thirdparty/gofer_session_list"}))
		assert.NoErr(t, run(t, false, "gofer_session_list"))
		assert.True(t, run(t, false, "gofer_issue_get") != nil)
		assert.True(t, run(t, false, "gofer_work_report") != nil)
		assert.NoErr(t, gkconfig.Set("tools.auto_allow", []string{"gofer_issue_get"}))
		assert.NoErr(t, run(t, false, "gofer_issue_get"))
	})
	t.Run("invalid server glob rejected", func(t *testing.T) {
		assert.NoErr(t, gkconfig.Set("tools.auto_allow", []string{"[/x"}))
		_, err := newCLIMCPGate(registry, false)
		assert.True(t, err != nil)
	})
}
