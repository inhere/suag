package acpserver

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
	"github.com/gookit/goutil/x/assert"
	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/mock"
)

// growingUsageProvider reports a larger input context on every model call, so
// "last request" and "sum of requests" are distinguishable.
type growingUsageProvider struct {
	*mock.Provider
	calls int
}

func (p *growingUsageProvider) Stream(ctx context.Context, req *agentcore.GenerateRequest, emit agentcore.StreamFunc) (*agentcore.GenerateResult, error) {
	result, err := p.Provider.Stream(ctx, req, emit)
	if result != nil {
		p.calls++
		in := 10 * p.calls
		result.Usage = agentcore.Usage{InputTokens: in, OutputTokens: 1, TotalTokens: in + 1, CostUSD: 0.5}
	}
	return result, err
}

func usageUpdates(client *testACPClient) []*acp.SessionUsageUpdate {
	client.mu.Lock()
	defer client.mu.Unlock()
	var out []*acp.SessionUsageUpdate
	for _, n := range client.updates {
		if n.Update.UsageUpdate != nil {
			out = append(out, n.Update.UsageUpdate)
		}
	}
	return out
}

func TestUsageUpdateIsSessionCumulative(t *testing.T) {
	provider := &growingUsageProvider{Provider: mock.NewProvider(mock.Final("one"), mock.Final("two"))}
	client := &testACPClient{}
	conn := connectPersonalACP(t, testACPRuntime(t, provider, nil), client)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	created, err := conn.NewSession(ctx, acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
	assert.NoErr(t, err)
	for _, prompt := range []string{"a", "b"} {
		_, err = conn.Prompt(ctx, acp.PromptRequest{SessionId: created.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock(prompt)}})
		assert.NoErr(t, err)
	}
	updates := usageUpdates(client)
	assert.Eq(t, 2, len(updates))

	t.Run("first turn", func(t *testing.T) {
		assert.Eq(t, float64(10), updates[0].Meta["input_tokens"])
		assert.Eq(t, 10, updates[0].Used)
	})
	t.Run("second turn accumulates", func(t *testing.T) {
		assert.Eq(t, float64(30), updates[1].Meta["input_tokens"]) // 10 + 20
		assert.Eq(t, float64(2), updates[1].Meta["output_tokens"])
		assert.Eq(t, float64(32), updates[1].Meta["total_tokens"])
		assert.Eq(t, float64(1), updates[1].Meta["cost_usd"])
		assert.Eq(t, float64(1), updates[1].Cost.Amount)
	})
	t.Run("used is last request context", func(t *testing.T) {
		assert.Eq(t, 20, updates[1].Used)
	})
	t.Run("size is positive", func(t *testing.T) {
		assert.Gt(t, updates[1].Size, 0)
	})
}

func TestCancelledTurnStillSendsUsageUpdate(t *testing.T) {
	provider := &blockingACPProvider{started: make(chan struct{})}
	client := &testACPClient{}
	conn := connectPersonalACP(t, testACPRuntime(t, provider, nil), client)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	created, err := conn.NewSession(ctx, acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
	assert.NoErr(t, err)
	done := make(chan acp.PromptResponse, 1)
	go func() {
		resp, _ := conn.Prompt(ctx, acp.PromptRequest{SessionId: created.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock("wait")}})
		done <- resp
	}()
	<-provider.started
	assert.NoErr(t, conn.Cancel(ctx, acp.CancelNotification{SessionId: created.SessionId}))
	assert.Eq(t, acp.StopReasonCancelled, (<-done).StopReason)
	updates := usageUpdates(client)
	assert.Eq(t, 1, len(updates))
	assert.Gt(t, updates[0].Size, 0)
}

func TestContextWindowForModel(t *testing.T) {
	cases := map[string]int{
		"claude-sonnet-4": 200000, "gpt-4o-mini": 128000, "gpt-4.1": 1000000,
		"deepseek-chat": 128000, "unknown-model": appconfig.DefaultContextWindow, "": appconfig.DefaultContextWindow,
	}
	for model, want := range cases {
		t.Run(model, func(t *testing.T) {
			assert.Eq(t, want, appconfig.ContextWindowForModel(model))
		})
	}
}

// Cumulative usage is persisted by personal.Runner, so a new ACP process that
// loads the session continues from the stored total instead of from zero.
func TestUsageUpdateContinuesAfterSessionLoad(t *testing.T) {
	provider := &growingUsageProvider{Provider: mock.NewProvider(mock.Final("one"), mock.Final("two"))}
	open := testACPRuntime(t, provider, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cwd := t.TempDir()

	firstClient := &testACPClient{}
	first := connectPersonalACP(t, open, firstClient)
	created, err := first.NewSession(ctx, acp.NewSessionRequest{Cwd: cwd, McpServers: []acp.McpServer{}})
	assert.NoErr(t, err)
	_, err = first.Prompt(ctx, acp.PromptRequest{SessionId: created.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock("a")}})
	assert.NoErr(t, err)
	assert.Eq(t, float64(10), usageUpdates(firstClient)[0].Meta["input_tokens"])

	// A second connection stands in for a restarted ACP process.
	secondClient := &testACPClient{}
	second := connectPersonalACP(t, open, secondClient)
	_, err = second.LoadSession(ctx, acp.LoadSessionRequest{SessionId: created.SessionId, Cwd: cwd, McpServers: []acp.McpServer{}})
	assert.NoErr(t, err)
	_, err = second.Prompt(ctx, acp.PromptRequest{SessionId: created.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock("b")}})
	assert.NoErr(t, err)
	updates := usageUpdates(secondClient)
	assert.Eq(t, 1, len(updates))
	assert.Eq(t, float64(30), updates[0].Meta["input_tokens"]) // 10 persisted + 20 new, not 20
	assert.Eq(t, float64(32), updates[0].Meta["total_tokens"])
}

// A first turn after session/load that fails before the runner finishes it must
// still report the persisted total, not reset the client to zero.
func TestUsageUpdateAfterSessionLoadEarlyFailure(t *testing.T) {
	provider := &growingUsageProvider{Provider: mock.NewProvider(mock.Final("one"), mock.Final("two"))}
	failRules := &atomic.Bool{}
	open := testACPRuntimeWith(t, provider, nil, acpTestOptions{failRules: failRules})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cwd := t.TempDir()

	first := connectPersonalACP(t, open, &testACPClient{})
	created, err := first.NewSession(ctx, acp.NewSessionRequest{Cwd: cwd, McpServers: []acp.McpServer{}})
	assert.NoErr(t, err)
	_, err = first.Prompt(ctx, acp.PromptRequest{SessionId: created.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock("a")}})
	assert.NoErr(t, err)

	secondClient := &testACPClient{}
	second := connectPersonalACP(t, open, secondClient)
	_, err = second.LoadSession(ctx, acp.LoadSessionRequest{SessionId: created.SessionId, Cwd: cwd, McpServers: []acp.McpServer{}})
	assert.NoErr(t, err)
	failRules.Store(true)
	_, err = second.Prompt(ctx, acp.PromptRequest{SessionId: created.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock("b")}})
	assert.Err(t, err)
	updates := usageUpdates(secondClient)
	assert.Eq(t, 1, len(updates))
	assert.Eq(t, float64(10), updates[0].Meta["input_tokens"]) // the persisted first-turn total
	assert.Eq(t, float64(11), updates[0].Meta["total_tokens"])
}

func TestUsageUpdateInputExcludesCache(t *testing.T) {
	u := usageUpdate(agentcore.Usage{InputTokens: 100, OutputTokens: 5, TotalTokens: 105, CacheReadTokens: 60, CacheWriteTokens: 30}, 100, 200000).UsageUpdate
	assert.Eq(t, 10, u.Meta["input_tokens"])
	assert.Eq(t, 60, u.Meta["cache_read_tokens"])
	assert.Eq(t, 30, u.Meta["cache_write_tokens"])
	assert.Eq(t, 105, u.Meta["total_tokens"])
}
