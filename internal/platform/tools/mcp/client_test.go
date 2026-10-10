//go:build !windows

package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"

	axconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/pkg/agentcore/api"
)

// TestFakeServerProcess is the fake stdio MCP server: the test binary re-executes
// itself with SUAG_FAKE_MCP_MODE set. Without the variable it is a no-op.
func TestFakeServerProcess(t *testing.T) {
	mode := os.Getenv("SUAG_FAKE_MCP_MODE")
	if mode == "" {
		return
	}
	if f := os.Getenv("SUAG_FAKE_MCP_PIDFILE"); f != "" {
		_ = os.WriteFile(f, []byte(strconv.Itoa(os.Getpid())), 0o600)
	}
	fmt.Fprintln(os.Stderr, "fake-mcp: boot diagnostics")
	switch mode {
	case "exit":
		os.Exit(3)
	case "silent":
		time.Sleep(time.Minute) // never answers initialize
		os.Exit(0)
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			os.Exit(2)
		}
		if mode == "crash-on-call" && req.Method == "tools/call" {
			os.Exit(4)
		}
		if mode == "hang-on-call" && req.Method == "tools/call" {
			continue // never answers a tool call
		}
		var result any = map[string]any{}
		if req.Method == "tools/list" {
			result = map[string]any{"tools": []map[string]any{{"name": "echo", "inputSchema": map[string]any{"type": "object"}}}}
		}
		out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		fmt.Println(string(out))
	}
	os.Exit(0)
}

func fakeConfig(t *testing.T, mode string) (ServerConfig, string) {
	t.Helper()
	pidFile := t.TempDir() + "/pid"
	return ServerConfig{
		Name:    "fake",
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestFakeServerProcess$"},
		Env:     []string{"SUAG_FAKE_MCP_MODE=" + mode, "SUAG_FAKE_MCP_PIDFILE=" + pidFile},
	}, pidFile
}

func fakeSettings(t *testing.T, mode string) (axconfig.MCPServerSettings, string) {
	cfg, pidFile := fakeConfig(t, mode)
	env := map[string]string{}
	for _, kv := range cfg.Env {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	return axconfig.MCPServerSettings{ID: cfg.Name, Command: cfg.Command, Args: cfg.Args, Env: env}, pidFile
}

// assertReaped fails unless the pid recorded by the fake server no longer exists
// at all; a zombie (killed but never waited) still answers signal 0.
func assertReaped(t *testing.T, pidFile string) {
	t.Helper()
	var pid int
	assert.True(t, waitFor(2*time.Second, func() bool {
		b, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(string(b))
		return err == nil
	}))
	assert.True(t, waitFor(2*time.Second, func() bool { return syscall.Kill(pid, 0) == syscall.ESRCH }))
}

func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

func setStartupTimeout(t *testing.T, d time.Duration) {
	prev := startupTimeout
	startupTimeout = d
	t.Cleanup(func() { startupTimeout = prev })
}

func TestNewProvider_StartupFailures(t *testing.T) {
	t.Run("child exits immediately fails fast with stderr", func(t *testing.T) {
		setStartupTimeout(t, 20*time.Second) // must not rely on the timeout
		settings, pidFile := fakeSettings(t, "exit")
		begin := time.Now()
		_, err := NewProvider(context.Background(), settings)
		assert.Err(t, err)
		assert.True(t, time.Since(begin) < 10*time.Second)
		assert.StrContains(t, err.Error(), `"fake"`)
		assert.StrContains(t, err.Error(), "fake-mcp: boot diagnostics")
		assertReaped(t, pidFile)
	})

	t.Run("child never answers initialize times out and is reaped", func(t *testing.T) {
		setStartupTimeout(t, 300*time.Millisecond)
		settings, pidFile := fakeSettings(t, "silent")
		begin := time.Now()
		_, err := NewProvider(context.Background(), settings)
		assert.Err(t, err)
		assert.True(t, time.Since(begin) < 10*time.Second)
		assert.StrContains(t, err.Error(), "timed out")
		assert.StrContains(t, err.Error(), `"fake"`)
		assertReaped(t, pidFile)
	})

	t.Run("ctx cancel returns immediately and reaps child", func(t *testing.T) {
		setStartupTimeout(t, time.Minute)
		settings, pidFile := fakeSettings(t, "silent")
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(200*time.Millisecond, cancel)
		begin := time.Now()
		_, err := NewProvider(ctx, settings)
		assert.Err(t, err)
		assert.True(t, time.Since(begin) < 10*time.Second)
		assert.StrContains(t, err.Error(), "context canceled")
		assertReaped(t, pidFile)
	})

	t.Run("healthy server still starts after startup ctx is released", func(t *testing.T) {
		settings, _ := fakeSettings(t, "ok")
		provider, err := NewProvider(context.Background(), settings)
		assert.NoErr(t, err)
		defer provider.Close()
		refs, err := provider.ListToolRefs(context.Background())
		assert.NoErr(t, err)
		assert.Eq(t, 1, len(refs))
	})
}

func TestClient_CallFailsWhenServerExits(t *testing.T) {
	cfg, pidFile := fakeConfig(t, "crash-on-call")
	client := NewClient(cfg)
	assert.NoErr(t, client.Start(context.Background()))
	defer client.Stop()

	t.Run("in-flight call fails when child dies", func(t *testing.T) {
		begin := time.Now()
		_, err := client.call(context.Background(), "tools/call", nil)
		assert.Err(t, err)
		assert.True(t, time.Since(begin) < 10*time.Second)
		assert.StrContains(t, err.Error(), "exited")
	})

	t.Run("later calls fail immediately", func(t *testing.T) {
		begin := time.Now()
		_, err := client.call(context.Background(), "tools/list", nil)
		assert.Err(t, err)
		assert.True(t, time.Since(begin) < 2*time.Second)
	})

	t.Run("stop reaps the child and is idempotent", func(t *testing.T) {
		client.Stop()
		client.Stop()
		assertReaped(t, pidFile)
	})
}

func TestToolCallTimeout(t *testing.T) {
	cfg, _ := fakeConfig(t, "hang-on-call")
	cfg.CallTimeout = 300 * time.Millisecond
	client := newClient(cfg)
	ctx := context.Background()
	assert.Require(t, assert.NoErr(t, client.Start(ctx)))
	t.Cleanup(client.Stop)
	defs, err := client.ListTools(ctx)
	assert.Require(t, assert.NoErr(t, err))
	start := time.Now()
	_, err = client.AsTool(defs[0]).Execute(ctx, api.ToolCall{CallID: "c", Name: "echo"})
	assert.ErrMsgContains(t, err, "did not answer within 300ms")
	assert.True(t, time.Since(start) < 3*time.Second)
}
