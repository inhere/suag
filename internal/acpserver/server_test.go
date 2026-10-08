package acpserver

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
	"github.com/gookit/goutil/x/assert"
	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/data/testutil"
	"github.com/inhere/suag/internal/host/session"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/mock"
)

type testACPClient struct {
	acp.Client
	mu            sync.Mutex
	selected      acp.PermissionOptionId
	permissionErr error
	updates       []acp.SessionNotification
	requests      []acp.RequestPermissionRequest
}

func (c *testACPClient) RequestPermission(_ context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, req)
	if c.permissionErr != nil {
		return acp.RequestPermissionResponse{}, c.permissionErr
	}
	return acp.RequestPermissionResponse{Outcome: acp.RequestPermissionOutcome{
		Selected: &acp.RequestPermissionOutcomeSelected{OptionId: c.selected},
	}}, nil
}

func (c *testACPClient) SessionUpdate(_ context.Context, update acp.SessionNotification) error {
	c.mu.Lock()
	c.updates = append(c.updates, update)
	c.mu.Unlock()
	return nil
}

func connectPersonalACP(t *testing.T, open OpenRuntime, client *testACPClient) *acp.ClientSideConnection {
	t.Helper()
	clientToAgentR, clientToAgentW := io.Pipe()
	agentToClientR, agentToClientW := io.Pipe()
	agent := New(open)
	agent.SetConnection(acp.NewAgentSideConnection(agent, agentToClientW, clientToAgentR))
	conn := acp.NewClientSideConnection(client, clientToAgentW, agentToClientR)
	t.Cleanup(func() {
		_ = clientToAgentW.Close()
		_ = clientToAgentR.Close()
		_ = agentToClientW.Close()
		_ = agentToClientR.Close()
	})
	return conn
}

func testACPRuntime(t *testing.T, provider agentcore.Provider, tool *countingTool) OpenRuntime {
	t.Helper()
	dbPath := testutil.NewMigratedSQLitePath(t, "sessions.db")
	runRoot := filepath.Join(t.TempDir(), "runs")
	return func(_ context.Context, _ string, _ bool, _ []appconfig.MCPServerSettings, hook agentcore.Hook) (Runtime, error) {
		repo, err := session.NewSQLiteRepository(dbPath)
		if err != nil {
			return Runtime{}, err
		}
		registry := tools.NewRegistry()
		if tool != nil {
			registry.Register(tool)
		}
		var hooks []agentcore.Hook
		if hook != nil {
			hooks = append(hooks, hook)
		}
		runner, err := personal.New(personal.Config{Repository: repo, RunRoot: runRoot, Provider: provider, Tools: registry, Hooks: hooks})
		if err != nil {
			_ = repo.Close()
			return Runtime{}, err
		}
		return Runtime{Runner: runner, Close: repo.Close}, nil
	}
}

type countingTool struct {
	calls     atomic.Int32
	writePath string
}

type countedUsageProvider struct{ *mock.Provider }

func (p countedUsageProvider) Stream(ctx context.Context, req *agentcore.GenerateRequest, emit agentcore.StreamFunc) (*agentcore.GenerateResult, error) {
	result, err := p.Provider.Stream(ctx, req, emit)
	if result != nil {
		result.Usage = agentcore.Usage{InputTokens: 7, OutputTokens: 3, TotalTokens: 10, CacheReadTokens: 2, CostUSD: 0.25}
	}
	return result, err
}

func (*countingTool) Name() string        { return "file_write" }
func (*countingTool) Description() string { return "write test file" }
func (*countingTool) Kind() tools.Kind    { return tools.KindNative }
func (*countingTool) Schema() agentcore.ToolSchema {
	return agentcore.ToolSchema{Name: "file_write", InputSchema: map[string]any{"type": "object"}}
}
func (t *countingTool) Execute(_ context.Context, call agentcore.ToolCall) (*agentcore.ToolResult, error) {
	t.calls.Add(1)
	if t.writePath != "" {
		content, _ := call.Arguments["content"].(string)
		if err := os.WriteFile(t.writePath, []byte(content), 0644); err != nil {
			return nil, err
		}
	}
	return &agentcore.ToolResult{CallID: call.CallID, Name: call.Name, Content: "written"}, nil
}

func TestPersonalACPRequiresPermissionBeforeToolExecution(t *testing.T) {
	for _, selected := range []acp.PermissionOptionId{"reject", "allow"} {
		t.Run(string(selected), func(t *testing.T) {
			tool := &countingTool{}
			provider := mock.NewProvider(mock.ToolCall("edit-1", "file_write", map[string]any{"path": "test.txt"}), mock.Final("done"))
			client := &testACPClient{selected: selected}
			conn := connectPersonalACP(t, testACPRuntime(t, provider, tool), client)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			created, err := conn.NewSession(ctx, acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
			if err != nil {
				t.Fatal(err)
			}
			response, err := conn.Prompt(ctx, acp.PromptRequest{SessionId: created.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock("edit")}})
			if err != nil {
				t.Fatal(err)
			}
			client.mu.Lock()
			requests := len(client.requests)
			started, terminal := false, false
			for _, update := range client.updates {
				started = started || update.Update.ToolCall != nil
				if status := update.Update.ToolCallUpdate; status != nil && status.Status != nil {
					terminal = terminal || *status.Status == acp.ToolCallStatusCompleted || *status.Status == acp.ToolCallStatusFailed
				}
			}
			client.mu.Unlock()
			if requests != 1 || !started || !terminal {
				t.Fatalf("permission requests = %d, tool started = %v, terminal update = %v", requests, started, terminal)
			}
			if selected == "reject" {
				if response.StopReason != acp.StopReasonRefusal || tool.calls.Load() != 0 {
					t.Fatalf("rejected tool: response=%s executions=%d", response.StopReason, tool.calls.Load())
				}
			} else if response.StopReason != acp.StopReasonEndTurn || tool.calls.Load() != 1 {
				t.Fatalf("allowed tool: response=%s executions=%d", response.StopReason, tool.calls.Load())
			}
		})
	}
}

func TestPersonalACPEmitsUsagePlanAndEditDetails(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, "test.txt")
	if err := os.WriteFile(path, []byte("before\n"), 0644); err != nil {
		t.Fatal(err)
	}
	tool := &countingTool{writePath: path}
	provider := countedUsageProvider{mock.NewProvider(
		mock.ToolCall("edit-1", "file_write", map[string]any{"path": "test.txt", "content": "after\n", "line": 2}),
		mock.Final("- [x] Inspect file\n- [ ] Verify edit"),
	)}
	client := &testACPClient{selected: "allow"}
	conn := connectPersonalACP(t, testACPRuntime(t, provider, tool), client)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	created, err := conn.NewSession(ctx, acp.NewSessionRequest{Cwd: cwd, McpServers: []acp.McpServer{}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := conn.Prompt(ctx, acp.PromptRequest{SessionId: created.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock("edit")}})
	if err != nil || response.StopReason != acp.StopReasonEndTurn {
		t.Fatalf("prompt = %+v, %v", response, err)
	}
	client.mu.Lock()
	updates := append([]acp.SessionNotification(nil), client.updates...)
	client.mu.Unlock()
	var usage *acp.SessionUsageUpdate
	var plan *acp.SessionUpdatePlan
	var started *acp.SessionUpdateToolCall
	var completed *acp.SessionToolCallUpdate
	for _, notification := range updates {
		update := notification.Update
		if update.UsageUpdate != nil {
			usage = update.UsageUpdate
		}
		if update.Plan != nil {
			plan = update.Plan
		}
		if update.ToolCall != nil {
			started = update.ToolCall
		}
		if update.ToolCallUpdate != nil && update.ToolCallUpdate.Status != nil && *update.ToolCallUpdate.Status == acp.ToolCallStatusCompleted {
			completed = update.ToolCallUpdate
		}
	}
	if usage == nil || plan == nil || started == nil || completed == nil {
		t.Fatalf("missing S4 updates: usage=%v plan=%v start=%v complete=%v", usage, plan, started, completed)
	}
	assert.Eq(t, acp.PlanEntryStatusCompleted, plan.Entries[0].Status)
	assert.Eq(t, acp.PlanEntryStatusPending, plan.Entries[1].Status)
	assert.Eq(t, acp.ToolKindEdit, started.Kind)
	assert.Eq(t, path, started.Locations[0].Path)
	assert.Eq(t, 2, *started.Locations[0].Line)
	assert.Eq(t, "before\n", *started.Content[0].Diff.OldText)
	assert.Eq(t, "after\n", started.Content[0].Diff.NewText)
	assert.Eq(t, acp.ToolKindEdit, *completed.Kind)
	assert.Eq(t, path, completed.Locations[0].Path)
	assert.Eq(t, "after\n", completed.Content[0].Diff.NewText)
	assert.Eq(t, float64(14), usage.Meta["input_tokens"])
	assert.Eq(t, float64(6), usage.Meta["output_tokens"])
	assert.Eq(t, float64(20), usage.Meta["total_tokens"])
	assert.Eq(t, float64(4), usage.Meta["cache_read_tokens"])
	assert.Eq(t, float64(0.5), usage.Meta["cost_usd"])
	assert.Eq(t, float64(0.5), usage.Cost.Amount)
}

func TestToolDiffRejectsWorkspaceEscapeSymlink(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("outside old contents\n"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(workspace, "linked.txt")
	if err := os.Symlink(outsideFile, link); err != nil {
		t.Skipf("cannot create symlink on this platform: %v", err)
	}

	locations := toolLocations(workspace, map[string]any{"path": "linked.txt"})
	diff := toolDiff(workspace, acp.ToolKindEdit, locations, map[string]any{"content": "replacement\n"})
	if len(diff) != 0 {
		t.Fatalf("workspace escape symlink produced diff: %+v", diff)
	}
}

func TestToolDiffKeepsNewFileDiff(t *testing.T) {
	workspace := t.TempDir()
	locations := toolLocations(workspace, map[string]any{"path": "new.txt"})
	diff := toolDiff(workspace, acp.ToolKindEdit, locations, map[string]any{"content": "new contents\n"})
	if len(diff) != 1 || diff[0].Diff == nil {
		t.Fatalf("new file diff = %+v", diff)
	}
	if diff[0].Diff.OldText != nil {
		t.Fatalf("new file old text = %q, want nil", *diff[0].Diff.OldText)
	}
	assert.Eq(t, "new contents\n", diff[0].Diff.NewText)
}

func TestToolDiffSupportsSymlinkedWorkspaceRoot(t *testing.T) {
	realWorkspace := t.TempDir()
	path := filepath.Join(realWorkspace, "existing.txt")
	if err := os.WriteFile(path, []byte("existing contents\n"), 0644); err != nil {
		t.Fatal(err)
	}
	workspaceLink := filepath.Join(t.TempDir(), "workspace")
	if err := os.Symlink(realWorkspace, workspaceLink); err != nil {
		t.Skipf("cannot create workspace symlink on this platform: %v", err)
	}

	locations := toolLocations(workspaceLink, map[string]any{"path": "existing.txt"})
	diff := toolDiff(workspaceLink, acp.ToolKindEdit, locations, map[string]any{"content": "replacement\n"})
	if len(diff) != 1 || diff[0].Diff == nil {
		t.Fatalf("symlinked workspace diff = %+v", diff)
	}
	if diff[0].Diff.OldText == nil {
		t.Fatal("symlinked workspace old text = nil, want existing contents")
	}
	assert.Eq(t, "existing contents\n", *diff[0].Diff.OldText)
}

func TestACPToolKinds(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind acp.ToolKind
	}{
		{"file_read", acp.ToolKindRead},
		{"file_write", acp.ToolKindEdit},
		{"shell_exec", acp.ToolKindExecute},
		{"web_search", acp.ToolKindSearch},
		{"web_fetch", acp.ToolKindFetch},
		{"unknown_mcp_tool", acp.ToolKindOther},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind, _ := toolKind(tc.name)
			assert.Eq(t, tc.kind, kind)
		})
	}
}

func TestPersonalACPReadOnlyRejectsToolWithoutPermissionRequest(t *testing.T) {
	tool := &countingTool{}
	client := &testACPClient{selected: "allow"}
	conn := connectPersonalACP(t, testACPRuntime(t, mock.NewProvider(mock.ToolCall("edit-1", "file_write", nil)), tool), client)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	created, err := conn.NewSession(ctx, acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.SetSessionMode(ctx, acp.SetSessionModeRequest{SessionId: created.SessionId, ModeId: "read_only"}); err != nil {
		t.Fatal(err)
	}
	response, err := conn.Prompt(ctx, acp.PromptRequest{SessionId: created.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock("edit")}})
	if err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	requests := len(client.requests)
	client.mu.Unlock()
	if response.StopReason != acp.StopReasonRefusal || tool.calls.Load() != 0 || requests != 0 {
		t.Fatalf("read_only: response=%s executions=%d permission requests=%d", response.StopReason, tool.calls.Load(), requests)
	}
}

func TestPersonalACPPermissionFailureDoesNotExecuteTool(t *testing.T) {
	tool := &countingTool{}
	client := &testACPClient{permissionErr: context.DeadlineExceeded}
	conn := connectPersonalACP(t, testACPRuntime(t, mock.NewProvider(mock.ToolCall("edit-1", "file_write", nil)), tool), client)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	created, err := conn.NewSession(ctx, acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := conn.Prompt(ctx, acp.PromptRequest{SessionId: created.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock("edit")}})
	if err != nil || response.StopReason != acp.StopReasonRefusal || tool.calls.Load() != 0 {
		t.Fatalf("permission failure: response=%+v error=%v executions=%d", response, err, tool.calls.Load())
	}
}

type recallACPProvider struct {
	mu    sync.Mutex
	calls int
}

func (*recallACPProvider) Name() string { return "recall" }
func (p *recallACPProvider) Generate(_ context.Context, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.calls > 1 {
		found := false
		for _, message := range req.Messages {
			found = found || message.Content == "amber"
		}
		if !found {
			return nil, errors.New("prior answer missing from resumed ACP context")
		}
	}
	return &agentcore.GenerateResult{Content: "amber", FinishReason: agentcore.FinishFinal}, nil
}

func TestPersonalACPTwoTurnsAndNewConnectionLoad(t *testing.T) {
	provider := &recallACPProvider{}
	open := testACPRuntime(t, provider, nil)
	client := &testACPClient{}
	conn := connectPersonalACP(t, open, client)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	initialized, err := conn.Initialize(ctx, acp.InitializeRequest{ProtocolVersion: acp.ProtocolVersionNumber})
	if err != nil || !initialized.AgentCapabilities.LoadSession {
		t.Fatalf("initialize: %+v, %v", initialized, err)
	}
	cwd := t.TempDir()
	created, err := conn.NewSession(ctx, acp.NewSessionRequest{Cwd: cwd, McpServers: []acp.McpServer{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{"remember amber", "what was the color?"} {
		response, err := conn.Prompt(ctx, acp.PromptRequest{SessionId: created.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock(prompt)}})
		if err != nil || response.StopReason != acp.StopReasonEndTurn {
			t.Fatalf("prompt %q: %+v, %v", prompt, response, err)
		}
	}
	newClient := &testACPClient{}
	newConn := connectPersonalACP(t, open, newClient)
	if _, err := newConn.LoadSession(ctx, acp.LoadSessionRequest{SessionId: created.SessionId, Cwd: cwd, McpServers: []acp.McpServer{}}); err != nil {
		t.Fatal(err)
	}
	response, err := newConn.Prompt(ctx, acp.PromptRequest{SessionId: created.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock("again?")}})
	if err != nil || response.StopReason != acp.StopReasonEndTurn {
		t.Fatalf("loaded prompt: %+v, %v", response, err)
	}
	newClient.mu.Lock()
	gotText := false
	for _, update := range newClient.updates {
		if update.Update.AgentMessageChunk != nil && update.Update.AgentMessageChunk.Content.Text != nil {
			gotText = gotText || strings.Contains(update.Update.AgentMessageChunk.Content.Text.Text, "amber")
		}
	}
	newClient.mu.Unlock()
	if !gotText {
		t.Fatal("loaded prompt did not emit answer update")
	}
}

type blockingACPProvider struct{ started chan struct{} }

func (*blockingACPProvider) Name() string { return "blocking" }
func (p *blockingACPProvider) Generate(ctx context.Context, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	close(p.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestPersonalACPCancelEndsActivePrompt(t *testing.T) {
	provider := &blockingACPProvider{started: make(chan struct{})}
	conn := connectPersonalACP(t, testACPRuntime(t, provider, nil), &testACPClient{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	created, err := conn.NewSession(ctx, acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan acp.PromptResponse, 1)
	errs := make(chan error, 1)
	go func() {
		response, runErr := conn.Prompt(ctx, acp.PromptRequest{SessionId: created.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock("wait")}})
		result <- response
		errs <- runErr
	}()
	select {
	case <-provider.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := conn.Cancel(ctx, acp.CancelNotification{SessionId: created.SessionId}); err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if response := <-result; response.StopReason != acp.StopReasonCancelled {
		t.Fatalf("stop reason = %s", response.StopReason)
	}
}

func TestACPMCPSettingsAcceptStdioAndRejectUnsupportedTransport(t *testing.T) {
	settings, err := mcpSettings([]acp.McpServer{{Stdio: &acp.McpServerStdio{
		Name: "local", Command: "mcp-server", Args: []string{"--stdio"},
		Env: []acp.EnvVariable{{Name: "MODE", Value: "test"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(settings) != 1 || settings[0].ID != "local" || settings[0].Command != "mcp-server" || settings[0].Env["MODE"] != "test" {
		t.Fatalf("stdio mapping = %+v", settings)
	}
	if _, err := mcpSettings([]acp.McpServer{{Http: &acp.McpServerHttpInline{}}}); err == nil {
		t.Fatal("unsupported HTTP MCP transport was accepted")
	}
}
