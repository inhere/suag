package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/inhere/suga/pkg/agentcore/api"
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
	Decide(ctx context.Context, call api.ToolCall, tool api.Tool) error
}

// ApproverFunc adapts a function to Approver.
type ApproverFunc func(ctx context.Context, call api.ToolCall, tool api.Tool) error

func (f ApproverFunc) Decide(ctx context.Context, call api.ToolCall, tool api.Tool) error {
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
	// disabled maps known tools this runtime did not register to why and how
	// to enable them; see SetDisabled.
	disabled map[string]string
}

// SetDisabled names the known tools that are off in this runtime (write tools
// in read-only mode, web_search without a key) with an enable hint. A call to
// one of them is refused with ErrDenied, so repeated attempts count toward the
// denial limit; any other unknown name is a plain error that does not count.
func (a *Approval) SetDisabled(hints map[string]string) { a.disabled = hints }

// maxListedTools bounds the available-tools list in a not-found error.
const maxListedTools = 40

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
func (a *Approval) Get(name string) (api.Tool, error) { return a.inner.Get(name) }

func (a *Approval) ExecuteTool(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	tool, err := a.inner.Get(call.Name)
	if err != nil {
		if hint, ok := a.disabled[call.Name]; ok {
			return nil, Denied(fmt.Sprintf("工具 %s 在本会话未启用：%s。不要重试，请告诉用户开启方法。", call.Name, hint))
		}
		return nil, fmt.Errorf("%w; available tools: %s", err, a.availableNames(ctx))
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
	if r := approvalReportOf(ctx); r != nil && r.Source == "" {
		r.Source = ApprovedByRule
	}
	return a.inner.ExecuteTool(ctx, call)
}

// availableNames lists the registered tool names for a not-found error.
func (a *Approval) availableNames(ctx context.Context) string {
	schemas, err := a.inner.ListTools(ctx)
	if err != nil || len(schemas) == 0 {
		return "(none)"
	}
	names := make([]string, 0, min(len(schemas), maxListedTools))
	for i, s := range schemas {
		if i == maxListedTools {
			names = append(names, fmt.Sprintf("… %d more", len(schemas)-i))
			break
		}
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
}

// Approval sources (ApprovalReport.Source).
const (
	// ApprovedByRule: the policy let the call run without asking (read-only
	// or agent-state tool, approval auto, an MCP rule).
	ApprovedByRule = "rule"
	// ApprovedByUser: the person was asked and allowed it.
	ApprovedByUser = "user"
	// ApprovedAlways: the person had answered "always" for this tool.
	ApprovedAlways = "always"
	// AskedAndDenied: the person was asked and refused.
	AskedAndDenied = "user_denied"
)

// ApprovalReport says how a call was approved: who decided and how long the
// person took to answer. The observing layer puts one in the context
// (WithApprovalReport) and reads it after the call; approvers that ask a
// person fill it in (ReportApproval); a call the Approver let through
// without reporting is ApprovedByRule.
type ApprovalReport struct {
	Source string
	Wait   time.Duration
	// Danger is why the call needed a stricter decision (a dangerous delete
	// command), "" for an ordinary call.
	Danger string
	// Rule is the stable summary of the representative approval rule. An empty
	// value means no declarative rule matched the call (the policy default ran).
	Rule string
	// Scope is the session rule's scope key; it is empty for user, project and
	// legacy rules, and for calls without a representative rule.
	Scope string
}

type approvalReportKey struct{}

// WithApprovalReport returns ctx carrying a fresh report.
func WithApprovalReport(ctx context.Context) (context.Context, *ApprovalReport) {
	r := &ApprovalReport{}
	return context.WithValue(ctx, approvalReportKey{}, r), r
}

// ReportApproval records the source and the person's answer time in ctx's
// report, if any.
func ReportApproval(ctx context.Context, source string, wait time.Duration) {
	if r, _ := ctx.Value(approvalReportKey{}).(*ApprovalReport); r != nil {
		r.Source, r.Wait = source, wait
	}
}

// ReportDanger records in ctx's report why the call is dangerous.
func ReportDanger(ctx context.Context, reason string) {
	if r, _ := ctx.Value(approvalReportKey{}).(*ApprovalReport); r != nil {
		r.Danger = reason
	}
}

func approvalReportOf(ctx context.Context) *ApprovalReport {
	r, _ := ctx.Value(approvalReportKey{}).(*ApprovalReport)
	return r
}
