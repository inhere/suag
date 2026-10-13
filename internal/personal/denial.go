package personal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/tools"
)

// DenialLimit is how many refused tool calls one turn may accumulate before the
// runner ends it (stream-json input_required, ACP stopReason refusal).
const DenialLimit = 3

type denialKey struct{}

// turnDenials counts the tool calls refused in one turn. It lives in the turn's
// ctx, so the count is reset by construction at every turn boundary; the
// observe layer (which sees tools.ErrDenied) adds to it.
type turnDenials struct {
	mu      sync.Mutex
	names   []string
	reached bool
}

func withTurnDenials(ctx context.Context) (context.Context, *turnDenials) {
	d := &turnDenials{}
	return context.WithValue(ctx, denialKey{}, d), d
}

func turnDenialsFrom(ctx context.Context) *turnDenials {
	d, _ := ctx.Value(denialKey{}).(*turnDenials)
	return d
}

func (d *turnDenials) add(name string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.names = append(d.names, name)
	d.mu.Unlock()
}

func (d *turnDenials) count() int {
	if d == nil {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.names)
}

// tools returns the distinct refused tool names in first-refusal order.
func (d *turnDenials) tools() []string {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []string
	seen := map[string]bool{}
	for _, name := range d.names {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func (d *turnDenials) markReached() {
	d.mu.Lock()
	d.reached = true
	d.mu.Unlock()
}

func (d *turnDenials) limitReached() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.reached
}

// denialLimitHook ends the turn before the next model call once DenialLimit
// refusals have accumulated. Ending in BeforeModel (not AfterTool, whose control
// agentcore does not support) keeps every refused call's tool result in history.
type denialLimitHook struct {
	agentcore.NopHook
	denials *turnDenials
	usage   *usageCollector
}

func (h denialLimitHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	if h.denials.count() < DenialLimit {
		return nil
	}
	h.denials.markReached()
	sum, _, _ := h.usage.snapshot()
	ctrl.ShortCircuit(&agentcore.RunResult{
		Status: agentcore.RunCompleted,
		Output: fmt.Sprintf("本回合已有 %d 次工具调用被拒绝（%s），回合结束，等待授权或新的指示。", h.denials.count(), strings.Join(h.denials.tools(), ", ")),
		Usage:  sum,
	})
	return nil
}

// abortingToolPolicy keeps the default "tool error becomes a tool result"
// behavior, except for an aborted approval or a cancelled turn: those fail the
// turn so a cancelled permission prompt is never fed back to the model.
type abortingToolPolicy struct {
	agentcore.DefaultToolExecutionPolicy
}

func (p abortingToolPolicy) HandleToolError(ctx context.Context, state *agentcore.RunState, call agentcore.ToolCall, err error) agentcore.ToolErrorDecision {
	if ctx.Err() != nil || errors.Is(err, tools.ErrAborted) {
		return agentcore.ToolErrorDecision{Fail: err}
	}
	return p.DefaultToolExecutionPolicy.HandleToolError(ctx, state, call, err)
}
