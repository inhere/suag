package personal

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore"
)

// toolUsing is a tool call reply reporting the given usage; finalUsing a
// final answer.
func toolUsing(id string, in, out int) (*agentcore.GenerateResult, error) {
	res, err := callTool(id)
	res.Usage = agentcore.Usage{InputTokens: in, OutputTokens: out}
	return res, err
}

func finalUsing(text string, in, out int) (*agentcore.GenerateResult, error) {
	res, err := finalText(text)
	res.Usage = agentcore.Usage{InputTokens: in, OutputTokens: out}
	return res, err
}

// stepClock is a manually advanced clock (Config.Now): a test sets the instant
// its scenario needs instead of waiting for real time to pass.
type stepClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stepClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestRunChild(t *testing.T) {
	repo := openObserveRepo(t)
	ctx := context.Background()
	assert.Require(t, assert.NoErr(t, repo.CreateSession(ctx, store.Session{ID: "parent", Status: store.SessionOpen})))
	child := func(t *testing.T, mut func(*Config), reply func(n int, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error)) (*Runner, *scriptProv) {
		prov := &scriptProv{name: "sub", normal: reply}
		return compactRunner(t, repo, prov, bigToolRegistry(10, nil), mut), prov
	}
	// withParentMeter models the parent turn the sub-agent runs inside: its usage
	// counts in the parent's run (usage.go) instead of being held for the
	// session's next run, which a leftover from an earlier -count run would
	// otherwise add to the usage of the first case.
	withParentMeter := func(t *testing.T) {
		t.Helper()
		t.Cleanup(activateMeter("parent", &runUsageMeter{}))
	}
	req := ChildRequest{ParentSession: "parent", ParentTurn: "turn_p", ParentCall: "call_9", Type: "explore",
		Description: "find callers", Prompt: "who calls Foo?"}

	t.Run("a final answer is the report; the session names its parent", func(t *testing.T) {
		runner, _ := child(t, nil, func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
			if n == 1 {
				return toolUsing("c1", 100, 10)
			}
			return finalUsing("Foo is called by Bar (a.go:3)", 150, 20)
		})
		var progress []Event
		r := req
		r.Emit = func(_ context.Context, e Event) error { progress = append(progress, e); return nil }
		parent := &runUsageMeter{}
		end := activateMeter("parent", parent)
		res, err := RunChild(ctx, runner, r)
		end()
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "Foo is called by Bar (a.go:3)", res.Report)
		assert.Eq(t, StopFinal, res.Stop)
		assert.Eq(t, 2, res.Steps)
		assert.Eq(t, 1, res.ToolCalls)
		assert.Eq(t, 250, res.Usage.InputTokens)
		assert.False(t, res.Tainted)
		assert.Eq(t, 250, parent.snapshot().Usage.InputTokens) // counted in the parent's run

		sess, err := repo.GetSession(ctx, res.SessionID)
		assert.Require(t, assert.NoErr(t, err))
		assert.True(t, store.IsAgentSession(sess))
		assert.Eq(t, "parent", store.ParentSession(sess))
		assert.Eq(t, "call_9", sess.Meta[metaParentCall])
		assert.Eq(t, "find callers", sess.Title)

		assert.Require(t, assert.Len(t, progress, 1)) // only its tool calls
		assert.Eq(t, "agent_progress", progress[0].Kind)
		assert.Eq(t, "big", progress[0].ToolName)
		assert.Eq(t, "find callers", progress[0].Agent)
	})
	t.Run("past the token budget it wraps up", func(t *testing.T) {
		withParentMeter(t)
		runner, prov := child(t, func(c *Config) { c.MaxRunTokens = 500 }, func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
			if n == 1 {
				return toolUsing("c1", 600, 10)
			}
			return finalText("what I have")
		})
		res, err := RunChild(ctx, runner, req)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, StopMaxTokens, res.Stop)
		assert.Eq(t, "what I have", res.Report)
		last := prov.reqs[1].Messages[len(prov.reqs[1].Messages)-1]
		assert.StrContains(t, last.Content, "token 预算（500）")
	})
	t.Run("a wrap-up it ignores leaves its last text as the report", func(t *testing.T) {
		withParentMeter(t)
		runner, _ := child(t, func(c *Config) { c.MaxRunTokens = 500 }, func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
			res, err := toolUsing("c"+strings.Repeat("x", n), 600, 10)
			res.Content = "so far: Bar calls Foo"
			return res, err
		})
		res, err := RunChild(ctx, runner, req)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, StopMaxTokens, res.Stop)
		assert.Eq(t, "so far: Bar calls Foo", res.Report)
	})
	// The clock is injected: the limit check must not depend on the real clock
	// moving on between the two reads (a coarse clock can report the same
	// instant twice, which made this case flaky).
	t.Run("past its time it wraps up", func(t *testing.T) {
		withParentMeter(t)
		clock := &stepClock{now: time.Unix(100, 0)}
		runner, prov := child(t, func(c *Config) { c.RunTimeout = time.Minute; c.Now = clock.Now }, func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
			if n == 1 {
				clock.advance(time.Hour) // this model call outlived the limit
				return callTool("c1")
			}
			return finalText("late answer")
		})
		res, err := RunChild(ctx, runner, req)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, StopTimeout, res.Stop)
		assert.Eq(t, "late answer", res.Report)
		// The wrap-up it was asked for is what reached the second request.
		last := prov.reqs[1].Messages[len(prov.reqs[1].Messages)-1]
		assert.StrContains(t, last.Content, "时间上限")
	})
	t.Run("a cancelled parent cancels it", func(t *testing.T) {
		withParentMeter(t)
		cctx, cancel := context.WithCancel(ctx)
		runner, _ := child(t, nil, func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
			cancel()
			return callTool("c1")
		})
		res, err := RunChild(cctx, runner, req)
		assert.Err(t, err)
		assert.Eq(t, "cancelled", res.Stop)
	})
}
