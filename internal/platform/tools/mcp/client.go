// Package mcp implements a JSON-RPC 2.0 client for MCP (Model Context Protocol) servers.
// It communicates with MCP servers via stdio (subprocess) or SSE transport.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/inhere/axon/internal/platform/tools"
	"github.com/inhere/axon/pkg/agentcore/api"
)

// ServerConfig describes an MCP server to connect to.
type ServerConfig struct {
	Name    string   // logical server name
	Command string   // executable
	Args    []string // command arguments
	Env     []string // extra environment variables (KEY=VALUE)
}

// Client connects to an MCP server via stdio and exposes its tools.
type Client struct {
	cfg     ServerConfig
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	mu      sync.Mutex
	seq     atomic.Int64
	pending map[int64]chan *jsonRPCResponse
}

// NewClient creates an MCP client from cfg but does not start the server yet.
func NewClient(cfg ServerConfig) *Client {
	return &Client{
		cfg:     cfg,
		pending: make(map[int64]chan *jsonRPCResponse),
	}
}

// Start launches the MCP server subprocess and performs the JSON-RPC initialize handshake.
func (c *Client) Start(ctx context.Context) error {
	c.cmd = exec.CommandContext(ctx, c.cfg.Command, c.cfg.Args...)
	c.cmd.Env = append(c.cmd.Env, c.cfg.Env...)

	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcp stdin pipe: %w", err)
	}
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mcp stdout pipe: %w", err)
	}

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("mcp start %q: %w", c.cfg.Command, err)
	}

	c.stdin = stdin
	c.scanner = bufio.NewScanner(stdout)
	go c.readLoop()

	// Handshake: initialize.
	_, err = c.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]any{"name": "axon", "version": "0.1.0"},
		"capabilities":    map[string]any{},
	})
	return err
}

// ListTools queries the MCP server for its available tools.
func (c *Client) ListTools(ctx context.Context) ([]mcpTool, error) {
	result, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Tools []mcpTool `json:"tools"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("parse tools/list: %w", err)
	}
	return resp.Tools, nil
}

// AsTool wraps an MCP tool definition as a tools.Tool for the registry.
func (c *Client) AsTool(t mcpTool) tools.Tool {
	return &mcpToolAdapter{client: c, def: t}
}

// Stop terminates the MCP server subprocess.
func (c *Client) Stop() {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
}

// ─── JSON-RPC internals ───────────────────────────────────────────────────────

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.seq.Add(1)
	ch := make(chan *jsonRPCResponse, 1)

	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	req := jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')

	c.mu.Lock()
	_, writeErr := c.stdin.Write(data)
	c.mu.Unlock()
	if writeErr != nil {
		return nil, fmt.Errorf("mcp write: %w", writeErr)
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("mcp error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

func (c *Client) readLoop() {
	for c.scanner.Scan() {
		line := c.scanner.Bytes()
		var resp jsonRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			slog.Warn("mcp: failed to parse response", "line", string(line), "err", err)
			continue
		}
		c.mu.Lock()
		ch, ok := c.pending[resp.ID]
		if ok {
			delete(c.pending, resp.ID)
		}
		c.mu.Unlock()
		if ok {
			ch <- &resp
		}
	}
}

// ─── mcpTool / adapter ────────────────────────────────────────────────────────

type mcpTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type mcpToolAdapter struct {
	client *Client
	def    mcpTool
}

func (a *mcpToolAdapter) Name() string        { return a.def.Name }
func (a *mcpToolAdapter) Description() string { return a.def.Description }
func (a *mcpToolAdapter) Kind() tools.Kind    { return tools.KindMCP }
func (a *mcpToolAdapter) Schema() api.ToolSchema {
	params := map[string]any{}
	_ = json.Unmarshal(a.def.InputSchema, &params)
	return api.ToolSchema{
		Name:        a.def.Name,
		Description: a.def.Description,
		InputSchema: params,
	}
}

func (a *mcpToolAdapter) Execute(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	result, err := a.client.call(ctx, "tools/call", map[string]any{
		"name":      a.def.Name,
		"arguments": call.Arguments,
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return &api.ToolResult{
			CallID:  call.CallID,
			Name:    a.def.Name,
			Content: string(result),
		}, nil
	}
	var buf bytes.Buffer
	for _, c := range resp.Content {
		buf.WriteString(c.Text)
	}
	return &api.ToolResult{
		CallID:  call.CallID,
		Name:    a.def.Name,
		Content: buf.String(),
	}, nil
}
