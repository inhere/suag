package agentcore_test

import (
	"context"
	"errors"
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
