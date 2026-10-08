package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/pkg/agentcore/api"
)

// approvalTool counts executions.
type approvalTool struct{ runs int }

func (*approvalTool) Name() string           { return "act" }
func (*approvalTool) Description() string    { return "act" }
func (*approvalTool) Schema() api.ToolSchema { return api.ToolSchema{Name: "act"} }
func (*approvalTool) Kind() Kind             { return KindNative }
func (t *approvalTool) Execute(context.Context, api.ToolCall) (*api.ToolResult, error) {
	t.runs++
	return &api.ToolResult{Content: "ok"}, nil
}

func TestApproval(t *testing.T) {
	call := api.ToolCall{CallID: "c1", Name: "act"}
	build := func(decide func() error) (*Approval, *approvalTool) {
		tool := &approvalTool{}
		reg := NewRegistry()
		reg.Register(tool)
		var approver Approver
		if decide != nil {
			approver = ApproverFunc(func(context.Context, api.ToolCall, Tool) error { return decide() })
		}
		return NewApproval(reg, approver), tool
	}

	t.Run("allow runs the tool", func(t *testing.T) {
		a, tool := build(func() error { return nil })
		res, err := a.ExecuteTool(context.Background(), call)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "ok", res.Content)
		assert.Eq(t, 1, tool.runs)
		got, err := a.Get("act")
		assert.NoErr(t, err)
		assert.Eq(t, "act", got.Name())
	})
	t.Run("denial is returned as is", func(t *testing.T) {
		a, tool := build(func() error { return Denied("no") })
		_, err := a.ExecuteTool(context.Background(), call)
		assert.Eq(t, true, errors.Is(err, ErrDenied))
		assert.Eq(t, false, errors.Is(err, ErrAborted))
		assert.Eq(t, "no", err.Error())
		assert.Eq(t, 0, tool.runs)
	})
	t.Run("other errors abort and keep the cause", func(t *testing.T) {
		a, tool := build(func() error { return context.Canceled })
		_, err := a.ExecuteTool(context.Background(), call)
		assert.Eq(t, true, errors.Is(err, ErrAborted))
		assert.Eq(t, true, errors.Is(err, context.Canceled))
		assert.Eq(t, false, errors.Is(err, ErrDenied))
		assert.Eq(t, 0, tool.runs)
	})
	t.Run("nil approver fails closed", func(t *testing.T) {
		a, tool := build(nil)
		_, err := a.ExecuteTool(context.Background(), call)
		assert.Eq(t, true, errors.Is(err, ErrDenied))
		assert.Eq(t, 0, tool.runs)
	})
	t.Run("unknown tool is not sent to the approver", func(t *testing.T) {
		asked := false
		reg := NewRegistry()
		a := NewApproval(reg, ApproverFunc(func(context.Context, api.ToolCall, Tool) error { asked = true; return nil }))
		_, err := a.ExecuteTool(context.Background(), api.ToolCall{Name: "missing"})
		assert.Err(t, err)
		assert.Eq(t, false, asked)
	})
}
