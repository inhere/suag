package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/inhere/suag/pkg/agentcore/api"
)

// ErrAborted marks an approval that could not be completed (the approver was
// cancelled or the turn context ended). Unlike ErrDenied it must end the turn:
// the runner's tool policy fails the turn instead of turning it into a tool result.
var ErrAborted = errors.New("tool approval aborted")

// abortedError keeps the original cause (context.Canceled, ...) reachable with
// errors.Is while also matching ErrAborted.
type abortedError struct{ err error }

func (e *abortedError) Error() string   { return e.err.Error() }
func (e *abortedError) Unwrap() []error { return []error{ErrAborted, e.err} }

// Approver decides whether one tool call may run.
//
// Decide returns nil to allow the call. An error matching ErrDenied (see
// Denied) refuses it: the model gets the message as a tool error and the turn
// continues. Any other error (typically context.Canceled from a cancelled
// approval prompt) aborts the turn; it is returned wrapped so it matches both
// ErrAborted and the original cause.
type Approver interface {
	Decide(ctx context.Context, call api.ToolCall, tool Tool) error
}

// ApproverFunc adapts a function to Approver.
type ApproverFunc func(ctx context.Context, call api.ToolCall, tool Tool) error

func (f ApproverFunc) Decide(ctx context.Context, call api.ToolCall, tool Tool) error {
	return f(ctx, call, tool)
}

// Approval is the permission layer of the tool registry stack (order:
// observe -> visibility -> approval -> base). Every call that reaches it is
// resolved against the base registry and handed to the Approver before it runs.
// Unloaded deferred tools never get here (visibility answers them first), so
// they never trigger an approval prompt.
type Approval struct {
	inner    lookupRegistry
	approver Approver
}

// NewApproval wraps inner with approver. A nil approver denies every call
// (fail closed), so a runtime opened without a policy cannot run tools.
func NewApproval(inner interface {
	api.ToolRegistry
	Lookup
}, approver Approver) *Approval {
	return &Approval{inner: inner, approver: approver}
}

func (a *Approval) ListTools(ctx context.Context) ([]api.ToolSchema, error) {
	return a.inner.ListTools(ctx)
}

// Get passes tool lookup through so outer layers can still classify tools.
func (a *Approval) Get(name string) (Tool, error) { return a.inner.Get(name) }

func (a *Approval) ExecuteTool(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	tool, err := a.inner.Get(call.Name)
	if err != nil {
		return nil, err
	}
	if a.approver == nil {
		return nil, Denied(fmt.Sprintf("tool %q is not permitted: no approval policy", call.Name))
	}
	if err := a.approver.Decide(ctx, call, tool); err != nil {
		if errors.Is(err, ErrDenied) {
			return nil, err
		}
		return nil, &abortedError{err: err}
	}
	return a.inner.ExecuteTool(ctx, call)
}
