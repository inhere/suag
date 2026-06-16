package agentcore_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/inhere/suag/pkg/agentcore"
	hookchain "github.com/inhere/suag/pkg/agentcore/hooks"
)

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

func (h controlStopHook) AfterTool(_ context.Context, _ *agentcore.ToolEvent, ctrl *agentcore.Control) error {
	*h.order = append(*h.order, h.name)
	if h.stop != nil {
		h.stop(ctrl)
	}
	return nil
}

func (h controlStopHook) OnError(_ context.Context, _ *agentcore.ErrorEvent, ctrl *agentcore.Control) error {
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

func TestHookChain_AfterToolControlDoesNotStopLaterHooks(t *testing.T) {
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
	)

	err := chain.AfterTool(context.Background(), &agentcore.ToolEvent{}, ctrl)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !reflect.DeepEqual(order, []string{"first", "second"}) {
		t.Fatalf("expected after-tool hooks to keep running after control, got %#v", order)
	}
	if !ctrl.ShouldShortCircuit() || ctrl.Replacement != "replacement" {
		t.Fatalf("expected control to still be observable after full after-tool stage, got %#v", ctrl)
	}
}

func TestHookChain_OnErrorObservedDoesNotStopLaterHooks(t *testing.T) {
	var order []string
	ctrl := agentcore.NewControl()
	chain := hookchain.NewChain(
		controlStopHook{
			name:  "first",
			order: &order,
			stop: func(ctrl *agentcore.Control) {
				ctrl.Yield("observe_only", nil)
			},
		},
		controlStopHook{name: "second", order: &order},
	)

	err := chain.OnErrorObserved(context.Background(), &agentcore.ErrorEvent{}, ctrl)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !reflect.DeepEqual(order, []string{"first", "second"}) {
		t.Fatalf("expected observed on-error hooks to keep running after control, got %#v", order)
	}
	if !ctrl.ShouldYield() || ctrl.YieldReason != "observe_only" {
		t.Fatalf("expected control to still be preserved for observation, got %#v", ctrl)
	}
}
