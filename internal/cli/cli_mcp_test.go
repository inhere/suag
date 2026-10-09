package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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
		// autoAllow sets tools.auto_allow (ignored by --read-only).
		autoAllow []string
	}{
		{name: "read tool executes", tool: "gofer_issue_list", readOnly: true},
		{name: "write stays denied with allow-write", tool: "gofer_issue_close", allowWrite: true, denied: true},
		{name: "chat uses MCP", tool: "gofer_issue_list", mode: "chat"},
		{name: "default MCP config", tool: "gofer_issue_list", defaultMCP: true},
		// The model keeps asking: the third refusal ends the turn before a 4th model call.
		{name: "three refusals end the turn", tool: "gofer_issue_close", denied: true, repeat: true},
		{name: "read-only refuses a user-allowed write tool", tool: "gofer_issue_close", readOnly: true, denied: true, autoAllow: []string{"gofer_*"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gkconfig.Reset()
			t.Cleanup(gkconfig.Reset)
			t.Setenv("SUAG_HOME", shortTestHome(t))
			workspace := t.TempDir()
			t.Chdir(workspace)
			assert.NoErr(t, gkconfig.Set("workspace", workspace))
			assert.NoErr(t, gkconfig.Set("llm.api_type", "openai"))
			assert.NoErr(t, gkconfig.Set("llm.api_key", "test-key"))
			if tc.autoAllow != nil {
				assert.NoErr(t, gkconfig.Set("tools.auto_allow", tc.autoAllow))
			}
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
			if tc.repeat {
				assert.Eq(t, 3, count)
			} else {
				assert.Eq(t, 2, count)
			}
			if mode == "run" {
				if tc.repeat {
					assert.Eq(t, true, strings.Contains(output.String(), `"input_tokens":6`))
					assert.StrContains(t, output.String(), "3 次工具调用被拒绝")
				} else {
					assert.Eq(t, true, strings.Contains(output.String(), `"input_tokens":4`))
					assert.Eq(t, true, strings.Contains(output.String(), `"result":"model-finished"`))
				}
				assert.Eq(t, tc.denied, strings.Contains(output.String(), `"type":"input_required"`))
				// The refused tools ride on input_required; the result stays COMPLETED.
				assert.Eq(t, tc.denied, strings.Contains(output.String(), `"denied_tools":["`+tc.tool+`"]`))
				assert.StrContains(t, output.String(), `"status":"COMPLETED"`)
			} else {
				assert.Eq(t, true, strings.Contains(output.String(), "model-finished"))
			}
		})
	}
}
