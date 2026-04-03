package agentcore_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/inhere/axon/pkg/agentcore"
	hookchain "github.com/inhere/axon/pkg/agentcore/hooks"
)

type testHook struct{}

func (testHook) BeforeRun(context.Context, *agentcore.RunEvent, *agentcore.Control) error {
	return nil
}
func (testHook) BeforePromptBuild(context.Context, *agentcore.PromptEvent, *agentcore.Control) error {
	return nil
}
func (testHook) BeforeContextBuild(context.Context, *agentcore.ContextEvent, *agentcore.Control) error {
	return nil
}
func (testHook) BeforeModel(context.Context, *agentcore.ModelEvent, *agentcore.Control) error {
	return nil
}
func (testHook) AfterModel(context.Context, *agentcore.ModelEvent, *agentcore.Control) error {
	return nil
}
func (testHook) BeforeTool(context.Context, *agentcore.ToolEvent, *agentcore.Control) error {
	return nil
}
func (testHook) AfterTool(context.Context, *agentcore.ToolEvent, *agentcore.Control) error {
	return nil
}
func (testHook) AfterRun(context.Context, *agentcore.RunEvent, *agentcore.Control) error {
	return nil
}
func (testHook) OnError(context.Context, *agentcore.ErrorEvent, *agentcore.Control) error {
	return nil
}

func TestHookLifecycleContracts_AreRepresentable(t *testing.T) {
	var _ agentcore.Hook = (*testHook)(nil)
}

func TestNopHook_ImplementsHook(t *testing.T) {
	var _ agentcore.Hook = agentcore.NopHook{}
}

func TestRunEvent_UsesRunResult(t *testing.T) {
	event := agentcore.RunEvent{
		RunID:   "run-1",
		AgentID: "agent-1",
		Input:   &agentcore.RunInput{RunID: "run-1", AgentID: "agent-1"},
		Result:  &agentcore.RunResult{RunID: "run-1", Status: agentcore.RunCompleted, Output: "done"},
	}

	if event.Result == nil || event.Result.RunID != "run-1" {
		t.Fatal("expected run event result to use RunResult")
	}
}

type mutatingPromptHook struct{}

func (mutatingPromptHook) BeforeRun(context.Context, *agentcore.RunEvent, *agentcore.Control) error {
	return nil
}
func (mutatingPromptHook) BeforePromptBuild(_ context.Context, event *agentcore.PromptEvent, _ *agentcore.Control) error {
	event.System = "after"
	return nil
}
func (mutatingPromptHook) BeforeContextBuild(context.Context, *agentcore.ContextEvent, *agentcore.Control) error {
	return nil
}
func (mutatingPromptHook) BeforeModel(context.Context, *agentcore.ModelEvent, *agentcore.Control) error {
	return nil
}
func (mutatingPromptHook) AfterModel(context.Context, *agentcore.ModelEvent, *agentcore.Control) error {
	return nil
}
func (mutatingPromptHook) BeforeTool(context.Context, *agentcore.ToolEvent, *agentcore.Control) error {
	return nil
}
func (mutatingPromptHook) AfterTool(context.Context, *agentcore.ToolEvent, *agentcore.Control) error {
	return nil
}
func (mutatingPromptHook) AfterRun(context.Context, *agentcore.RunEvent, *agentcore.Control) error {
	return nil
}
func (mutatingPromptHook) OnError(context.Context, *agentcore.ErrorEvent, *agentcore.Control) error {
	return nil
}

type mutatingContextHook struct{}

func (mutatingContextHook) BeforeRun(context.Context, *agentcore.RunEvent, *agentcore.Control) error {
	return nil
}
func (mutatingContextHook) BeforePromptBuild(context.Context, *agentcore.PromptEvent, *agentcore.Control) error {
	return nil
}
func (mutatingContextHook) BeforeContextBuild(_ context.Context, event *agentcore.ContextEvent, _ *agentcore.Control) error {
	event.Messages = []agentcore.Message{{Role: "assistant", Content: "after"}}
	return nil
}
func (mutatingContextHook) BeforeModel(context.Context, *agentcore.ModelEvent, *agentcore.Control) error {
	return nil
}
func (mutatingContextHook) AfterModel(context.Context, *agentcore.ModelEvent, *agentcore.Control) error {
	return nil
}
func (mutatingContextHook) BeforeTool(context.Context, *agentcore.ToolEvent, *agentcore.Control) error {
	return nil
}
func (mutatingContextHook) AfterTool(context.Context, *agentcore.ToolEvent, *agentcore.Control) error {
	return nil
}
func (mutatingContextHook) AfterRun(context.Context, *agentcore.RunEvent, *agentcore.Control) error {
	return nil
}
func (mutatingContextHook) OnError(context.Context, *agentcore.ErrorEvent, *agentcore.Control) error {
	return nil
}

type mutatingModelHook struct{}

func (mutatingModelHook) BeforeRun(context.Context, *agentcore.RunEvent, *agentcore.Control) error {
	return nil
}
func (mutatingModelHook) BeforePromptBuild(context.Context, *agentcore.PromptEvent, *agentcore.Control) error {
	return nil
}
func (mutatingModelHook) BeforeContextBuild(context.Context, *agentcore.ContextEvent, *agentcore.Control) error {
	return nil
}
func (mutatingModelHook) BeforeModel(_ context.Context, event *agentcore.ModelEvent, _ *agentcore.Control) error {
	event.Request = &agentcore.GenerateRequest{Model: "mutated"}
	return nil
}
func (mutatingModelHook) AfterModel(context.Context, *agentcore.ModelEvent, *agentcore.Control) error {
	return nil
}
func (mutatingModelHook) BeforeTool(context.Context, *agentcore.ToolEvent, *agentcore.Control) error {
	return nil
}
func (mutatingModelHook) AfterTool(context.Context, *agentcore.ToolEvent, *agentcore.Control) error {
	return nil
}
func (mutatingModelHook) AfterRun(context.Context, *agentcore.RunEvent, *agentcore.Control) error {
	return nil
}
func (mutatingModelHook) OnError(context.Context, *agentcore.ErrorEvent, *agentcore.Control) error {
	return nil
}

type mutatingToolHook struct{}

func (mutatingToolHook) BeforeRun(context.Context, *agentcore.RunEvent, *agentcore.Control) error {
	return nil
}
func (mutatingToolHook) BeforePromptBuild(context.Context, *agentcore.PromptEvent, *agentcore.Control) error {
	return nil
}
func (mutatingToolHook) BeforeContextBuild(context.Context, *agentcore.ContextEvent, *agentcore.Control) error {
	return nil
}
func (mutatingToolHook) BeforeModel(context.Context, *agentcore.ModelEvent, *agentcore.Control) error {
	return nil
}
func (mutatingToolHook) AfterModel(context.Context, *agentcore.ModelEvent, *agentcore.Control) error {
	return nil
}
func (mutatingToolHook) BeforeTool(_ context.Context, event *agentcore.ToolEvent, _ *agentcore.Control) error {
	event.Call = &agentcore.ToolCall{CallID: "call-2", Name: "mutated"}
	return nil
}
func (mutatingToolHook) AfterTool(context.Context, *agentcore.ToolEvent, *agentcore.Control) error {
	return nil
}
func (mutatingToolHook) AfterRun(context.Context, *agentcore.RunEvent, *agentcore.Control) error {
	return nil
}
func (mutatingToolHook) OnError(context.Context, *agentcore.ErrorEvent, *agentcore.Control) error {
	return nil
}

func TestPromptEvent_IsMutablySharedThroughHook(t *testing.T) {
	event := &agentcore.PromptEvent{
		RunID:   "run-1",
		AgentID: "agent-1",
		System:  "before",
	}

	hook := mutatingPromptHook{}
	if err := hook.BeforePromptBuild(context.Background(), event, agentcore.NewControl()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.System != "after" {
		t.Fatalf("expected prompt event mutation to persist, got %q", event.System)
	}
}

func TestContextEvent_IsMutablySharedThroughHook(t *testing.T) {
	event := &agentcore.ContextEvent{
		RunID:    "run-1",
		AgentID:  "agent-1",
		Messages: []agentcore.Message{{Role: "user", Content: "before"}},
	}

	if len(event.Messages) != 1 {
		t.Fatal("expected initial message")
	}

	hook := mutatingContextHook{}
	if err := hook.BeforeContextBuild(context.Background(), event, agentcore.NewControl()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(event.Messages) != 1 || event.Messages[0].Content != "after" {
		t.Fatalf("expected context event message replacement to persist, got %#v", event.Messages)
	}
}

func TestModelEvent_RequestIsMutablySharedThroughHook(t *testing.T) {
	event := &agentcore.ModelEvent{
		RunID:   "run-1",
		AgentID: "agent-1",
		Request: &agentcore.GenerateRequest{Model: "before"},
	}

	hook := mutatingModelHook{}
	if err := hook.BeforeModel(context.Background(), event, agentcore.NewControl()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Request == nil || event.Request.Model != "mutated" {
		t.Fatalf("expected model event request mutation to persist, got %#v", event.Request)
	}
}

func TestToolEvent_CallIsMutablySharedThroughHook(t *testing.T) {
	event := &agentcore.ToolEvent{
		RunID:   "run-1",
		AgentID: "agent-1",
		Call:    &agentcore.ToolCall{CallID: "call-1", Name: "before"},
	}

	hook := mutatingToolHook{}
	if err := hook.BeforeTool(context.Background(), event, agentcore.NewControl()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Call == nil || event.Call.Name != "mutated" {
		t.Fatalf("expected tool event call mutation to persist, got %#v", event.Call)
	}
}

type orderedHook struct {
	agentcore.NopHook
	name  string
	order *[]string
	err   error
}

func (h orderedHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, _ *agentcore.Control) error {
	*h.order = append(*h.order, h.name)
	return h.err
}

func TestHookChain_BeforeModelRunsHooksInOrder(t *testing.T) {
	var order []string
	chain := hookchain.NewChain(
		orderedHook{name: "first", order: &order},
		orderedHook{name: "second", order: &order},
	)

	err := chain.BeforeModel(context.Background(), &agentcore.ModelEvent{}, agentcore.NewControl())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !reflect.DeepEqual(order, []string{"first", "second"}) {
		t.Fatalf("expected hooks to run in order, got %#v", order)
	}
}

func TestHookChain_StopsOnFirstError(t *testing.T) {
	var order []string
	wantErr := errors.New("stop")
	chain := hookchain.NewChain(
		orderedHook{name: "first", order: &order},
		orderedHook{name: "second", order: &order, err: wantErr},
		orderedHook{name: "third", order: &order},
	)

	err := chain.BeforeModel(context.Background(), &agentcore.ModelEvent{}, agentcore.NewControl())
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
	if !reflect.DeepEqual(order, []string{"first", "second"}) {
		t.Fatalf("expected execution to stop after error, got %#v", order)
	}
}

type chainRecorderHook struct {
	agentcore.NopHook
	name            string
	order           *[]string
	targetMethod    string
	errOnMethod     string
	err             error
	shortCircuitOn  string
	assertControlOn string
}

func (h chainRecorderHook) record(method string) error {
	if method == h.targetMethod {
		*h.order = append(*h.order, h.name)
	}
	if method == h.shortCircuitOn {
		return nil
	}
	if method == h.errOnMethod {
		return h.err
	}
	return nil
}

func (h chainRecorderHook) maybeShortCircuit(method string, ctrl *agentcore.Control) {
	if method == h.shortCircuitOn {
		ctrl.ShortCircuit("replacement")
	}
}

func (h chainRecorderHook) assertControl(method string, ctrl *agentcore.Control) error {
	if method != h.assertControlOn {
		return nil
	}
	if !ctrl.ShouldShortCircuit() || ctrl.Replacement != "replacement" {
		return fmt.Errorf("expected shared short-circuit control, got %#v", ctrl)
	}
	return nil
}

func (h chainRecorderHook) BeforeRun(_ context.Context, _ *agentcore.RunEvent, ctrl *agentcore.Control) error {
	h.maybeShortCircuit("BeforeRun", ctrl)
	if err := h.assertControl("BeforeRun", ctrl); err != nil {
		return err
	}
	return h.record("BeforeRun")
}

func (h chainRecorderHook) BeforePromptBuild(_ context.Context, _ *agentcore.PromptEvent, ctrl *agentcore.Control) error {
	h.maybeShortCircuit("BeforePromptBuild", ctrl)
	if err := h.assertControl("BeforePromptBuild", ctrl); err != nil {
		return err
	}
	return h.record("BeforePromptBuild")
}

func (h chainRecorderHook) BeforeContextBuild(_ context.Context, _ *agentcore.ContextEvent, ctrl *agentcore.Control) error {
	h.maybeShortCircuit("BeforeContextBuild", ctrl)
	if err := h.assertControl("BeforeContextBuild", ctrl); err != nil {
		return err
	}
	return h.record("BeforeContextBuild")
}

func (h chainRecorderHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	h.maybeShortCircuit("BeforeModel", ctrl)
	if err := h.assertControl("BeforeModel", ctrl); err != nil {
		return err
	}
	return h.record("BeforeModel")
}

func (h chainRecorderHook) AfterModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	h.maybeShortCircuit("AfterModel", ctrl)
	if err := h.assertControl("AfterModel", ctrl); err != nil {
		return err
	}
	return h.record("AfterModel")
}

func (h chainRecorderHook) BeforeTool(_ context.Context, _ *agentcore.ToolEvent, ctrl *agentcore.Control) error {
	h.maybeShortCircuit("BeforeTool", ctrl)
	if err := h.assertControl("BeforeTool", ctrl); err != nil {
		return err
	}
	return h.record("BeforeTool")
}

func (h chainRecorderHook) AfterTool(_ context.Context, _ *agentcore.ToolEvent, ctrl *agentcore.Control) error {
	h.maybeShortCircuit("AfterTool", ctrl)
	if err := h.assertControl("AfterTool", ctrl); err != nil {
		return err
	}
	return h.record("AfterTool")
}

func (h chainRecorderHook) AfterRun(_ context.Context, _ *agentcore.RunEvent, ctrl *agentcore.Control) error {
	h.maybeShortCircuit("AfterRun", ctrl)
	if err := h.assertControl("AfterRun", ctrl); err != nil {
		return err
	}
	return h.record("AfterRun")
}

func (h chainRecorderHook) OnError(_ context.Context, _ *agentcore.ErrorEvent, ctrl *agentcore.Control) error {
	h.maybeShortCircuit("OnError", ctrl)
	if err := h.assertControl("OnError", ctrl); err != nil {
		return err
	}
	return h.record("OnError")
}

type controlStopHook struct {
	agentcore.NopHook
	name  string
	order *[]string
	stop  func(*agentcore.Control)
}

func (h controlStopHook) BeforePromptBuild(_ context.Context, _ *agentcore.PromptEvent, ctrl *agentcore.Control) error {
	*h.order = append(*h.order, h.name)
	if h.stop != nil {
		h.stop(ctrl)
	}
	return nil
}

func TestHookChain_MethodsRunHooksInOrder(t *testing.T) {
	type testCase struct {
		name   string
		method string
		invoke func(hookchain.Chain, *agentcore.Control) error
	}

	tests := []testCase{
		{
			name:   "BeforeRun",
			method: "BeforeRun",
			invoke: func(chain hookchain.Chain, ctrl *agentcore.Control) error {
				return chain.BeforeRun(context.Background(), &agentcore.RunEvent{}, ctrl)
			},
		},
		{
			name:   "BeforePromptBuild",
			method: "BeforePromptBuild",
			invoke: func(chain hookchain.Chain, ctrl *agentcore.Control) error {
				return chain.BeforePromptBuild(context.Background(), &agentcore.PromptEvent{}, ctrl)
			},
		},
		{
			name:   "BeforeContextBuild",
			method: "BeforeContextBuild",
			invoke: func(chain hookchain.Chain, ctrl *agentcore.Control) error {
				return chain.BeforeContextBuild(context.Background(), &agentcore.ContextEvent{}, ctrl)
			},
		},
		{
			name:   "AfterModel",
			method: "AfterModel",
			invoke: func(chain hookchain.Chain, ctrl *agentcore.Control) error {
				return chain.AfterModel(context.Background(), &agentcore.ModelEvent{}, ctrl)
			},
		},
		{
			name:   "BeforeTool",
			method: "BeforeTool",
			invoke: func(chain hookchain.Chain, ctrl *agentcore.Control) error {
				return chain.BeforeTool(context.Background(), &agentcore.ToolEvent{}, ctrl)
			},
		},
		{
			name:   "AfterTool",
			method: "AfterTool",
			invoke: func(chain hookchain.Chain, ctrl *agentcore.Control) error {
				return chain.AfterTool(context.Background(), &agentcore.ToolEvent{}, ctrl)
			},
		},
		{
			name:   "AfterRun",
			method: "AfterRun",
			invoke: func(chain hookchain.Chain, ctrl *agentcore.Control) error {
				return chain.AfterRun(context.Background(), &agentcore.RunEvent{}, ctrl)
			},
		},
		{
			name:   "OnError",
			method: "OnError",
			invoke: func(chain hookchain.Chain, ctrl *agentcore.Control) error {
				return chain.OnError(context.Background(), &agentcore.ErrorEvent{}, ctrl)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var order []string
			chain := hookchain.NewChain(
				chainRecorderHook{name: "first", order: &order, targetMethod: tc.method},
				chainRecorderHook{name: "second", order: &order, targetMethod: tc.method},
			)

			err := tc.invoke(chain, agentcore.NewControl())
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if !reflect.DeepEqual(order, []string{"first", "second"}) {
				t.Fatalf("expected hooks to run in order, got %#v", order)
			}
		})
	}
}

func TestHookChain_BeforePromptBuildShortCircuitStopsLaterSameStageHooks(t *testing.T) {
	var order []string
	ctrl := agentcore.NewControl()
	chain := hookchain.NewChain(
		controlStopHook{
			name:  "first",
			order: &order,
			stop: func(ctrl *agentcore.Control) {
				ctrl.ShortCircuit("replacement")
			},
		},
		controlStopHook{name: "second", order: &order},
		controlStopHook{name: "third", order: &order},
	)

	err := chain.BeforePromptBuild(context.Background(), &agentcore.PromptEvent{}, ctrl)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !reflect.DeepEqual(order, []string{"first"}) {
		t.Fatalf("expected short-circuit to stop later hooks, got %#v", order)
	}
	if !ctrl.ShouldShortCircuit() || ctrl.Replacement != "replacement" {
		t.Fatalf("expected short-circuit control to be preserved, got %#v", ctrl)
	}
}

func TestHookChain_BeforePromptBuildYieldStopsLaterSameStageHooks(t *testing.T) {
	var order []string
	ctrl := agentcore.NewControl()
	chain := hookchain.NewChain(
		controlStopHook{
			name:  "first",
			order: &order,
			stop: func(ctrl *agentcore.Control) {
				ctrl.Yield("await_approval", map[string]any{"policy": "manual"})
			},
		},
		controlStopHook{name: "second", order: &order},
		controlStopHook{name: "third", order: &order},
	)

	err := chain.BeforePromptBuild(context.Background(), &agentcore.PromptEvent{}, ctrl)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !reflect.DeepEqual(order, []string{"first"}) {
		t.Fatalf("expected yield to stop later hooks, got %#v", order)
	}
	if !ctrl.ShouldYield() || ctrl.YieldReason != "await_approval" {
		t.Fatalf("expected yield control to be preserved, got %#v", ctrl)
	}
}
