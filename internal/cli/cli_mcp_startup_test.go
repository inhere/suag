package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
)

// TestPersonalRunMCPStartupNeverHangs covers the S3 contract: a broken MCP server
// makes `run` fail (or stop on cancel) in bounded time instead of hanging.
func TestPersonalRunMCPStartupNeverHangs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		command string
		args    []string
		cancel  bool // cancel the caller ctx shortly after start
	}{
		{name: "server exits immediately", command: "false"},
		{name: "server never answers and ctx is cancelled", command: "sleep", args: []string{"100"}, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gkconfig.Reset()
			t.Cleanup(gkconfig.Reset)
			t.Setenv("SUGA_HOME", shortTestHome(t))
			workspace := t.TempDir()
			t.Chdir(workspace)
			assert.NoErr(t, gkconfig.Set("workspace", workspace))
			assert.NoErr(t, gkconfig.Set("model", "openai/gpt-4o"))
			setTestProvider(t, "http://127.0.0.1:1")

			mcpConfig := filepath.Join(t.TempDir(), "bad.json")
			config, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"bad": map[string]any{
				"command": tc.command, "args": tc.args,
			}}})
			assert.NoErr(t, err)
			assert.NoErr(t, os.WriteFile(mcpConfig, config, 0o600))

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				time.AfterFunc(300*time.Millisecond, cancel)
			}
			errCh := make(chan error, 1)
			go func() {
				var out, diag bytes.Buffer
				errCh <- RunPersonalCLI(ctx, "run", []string{"--prompt", "hi", "--mcp-config", mcpConfig}, strings.NewReader(""), &out, &diag)
			}()
			select {
			case err := <-errCh:
				assert.Err(t, err)
				assert.StrContains(t, err.Error(), `"bad"`)
			case <-time.After(15 * time.Second):
				t.Fatal("run hung on MCP startup failure")
			}
		})
	}
}
