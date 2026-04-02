package agentcore

import (
	"context"
	"testing"
)

type testHook struct{}

func (testHook) BeforeRun(context.Context, *RunEvent, *Control) error              { return nil }
func (testHook) BeforePromptBuild(context.Context, *PromptEvent, *Control) error   { return nil }
func (testHook) BeforeContextBuild(context.Context, *ContextEvent, *Control) error { return nil }
func (testHook) BeforeModel(context.Context, *ModelEvent, *Control) error          { return nil }
func (testHook) AfterModel(context.Context, *ModelEvent, *Control) error           { return nil }
func (testHook) BeforeTool(context.Context, *ToolEvent, *Control) error            { return nil }
func (testHook) AfterTool(context.Context, *ToolEvent, *Control) error             { return nil }
func (testHook) AfterRun(context.Context, *RunEvent, *Control) error               { return nil }
func (testHook) OnError(context.Context, *ErrorEvent, *Control) error              { return nil }

func TestHookLifecycleContracts_AreRepresentable(t *testing.T) {
	var _ Hook = (*testHook)(nil)
}

func TestNopHook_ImplementsHook(t *testing.T) {
	var _ Hook = NopHook{}
}

func TestRunEvent_UsesRunResult(t *testing.T) {
	event := RunEvent{
		RunID:   "run-1",
		AgentID: "agent-1",
		Input:   &RunInput{RunID: "run-1", AgentID: "agent-1"},
		Result:  &RunResult{RunID: "run-1", Status: RunCompleted, Output: "done"},
	}

	if event.Result == nil || event.Result.RunID != "run-1" {
		t.Fatal("expected run event result to use RunResult")
	}
}

type mutatingPromptHook struct{}

func (mutatingPromptHook) BeforeRun(context.Context, *RunEvent, *Control) error { return nil }
func (mutatingPromptHook) BeforePromptBuild(_ context.Context, event *PromptEvent, _ *Control) error {
	event.System = "after"
	return nil
}
func (mutatingPromptHook) BeforeContextBuild(context.Context, *ContextEvent, *Control) error {
	return nil
}
func (mutatingPromptHook) BeforeModel(context.Context, *ModelEvent, *Control) error { return nil }
func (mutatingPromptHook) AfterModel(context.Context, *ModelEvent, *Control) error  { return nil }
func (mutatingPromptHook) BeforeTool(context.Context, *ToolEvent, *Control) error   { return nil }
func (mutatingPromptHook) AfterTool(context.Context, *ToolEvent, *Control) error    { return nil }
func (mutatingPromptHook) AfterRun(context.Context, *RunEvent, *Control) error      { return nil }
func (mutatingPromptHook) OnError(context.Context, *ErrorEvent, *Control) error     { return nil }

type mutatingContextHook struct{}

func (mutatingContextHook) BeforeRun(context.Context, *RunEvent, *Control) error { return nil }
func (mutatingContextHook) BeforePromptBuild(context.Context, *PromptEvent, *Control) error {
	return nil
}
func (mutatingContextHook) BeforeContextBuild(_ context.Context, event *ContextEvent, _ *Control) error {
	event.Messages = []Message{{Role: "assistant", Content: "after"}}
	return nil
}
func (mutatingContextHook) BeforeModel(context.Context, *ModelEvent, *Control) error { return nil }
func (mutatingContextHook) AfterModel(context.Context, *ModelEvent, *Control) error  { return nil }
func (mutatingContextHook) BeforeTool(context.Context, *ToolEvent, *Control) error   { return nil }
func (mutatingContextHook) AfterTool(context.Context, *ToolEvent, *Control) error    { return nil }
func (mutatingContextHook) AfterRun(context.Context, *RunEvent, *Control) error      { return nil }
func (mutatingContextHook) OnError(context.Context, *ErrorEvent, *Control) error     { return nil }

type mutatingModelHook struct{}

func (mutatingModelHook) BeforeRun(context.Context, *RunEvent, *Control) error            { return nil }
func (mutatingModelHook) BeforePromptBuild(context.Context, *PromptEvent, *Control) error { return nil }
func (mutatingModelHook) BeforeContextBuild(context.Context, *ContextEvent, *Control) error {
	return nil
}
func (mutatingModelHook) BeforeModel(_ context.Context, event *ModelEvent, _ *Control) error {
	event.Request = &GenerateRequest{Model: "mutated"}
	return nil
}
func (mutatingModelHook) AfterModel(context.Context, *ModelEvent, *Control) error { return nil }
func (mutatingModelHook) BeforeTool(context.Context, *ToolEvent, *Control) error  { return nil }
func (mutatingModelHook) AfterTool(context.Context, *ToolEvent, *Control) error   { return nil }
func (mutatingModelHook) AfterRun(context.Context, *RunEvent, *Control) error     { return nil }
func (mutatingModelHook) OnError(context.Context, *ErrorEvent, *Control) error    { return nil }

type mutatingToolHook struct{}

func (mutatingToolHook) BeforeRun(context.Context, *RunEvent, *Control) error            { return nil }
func (mutatingToolHook) BeforePromptBuild(context.Context, *PromptEvent, *Control) error { return nil }
func (mutatingToolHook) BeforeContextBuild(context.Context, *ContextEvent, *Control) error {
	return nil
}
func (mutatingToolHook) BeforeModel(context.Context, *ModelEvent, *Control) error { return nil }
func (mutatingToolHook) AfterModel(context.Context, *ModelEvent, *Control) error  { return nil }
func (mutatingToolHook) BeforeTool(_ context.Context, event *ToolEvent, _ *Control) error {
	event.Call = &ToolCall{CallID: "call-2", Name: "mutated"}
	return nil
}
func (mutatingToolHook) AfterTool(context.Context, *ToolEvent, *Control) error { return nil }
func (mutatingToolHook) AfterRun(context.Context, *RunEvent, *Control) error   { return nil }
func (mutatingToolHook) OnError(context.Context, *ErrorEvent, *Control) error  { return nil }

func TestPromptEvent_IsMutablySharedThroughHook(t *testing.T) {
	event := &PromptEvent{
		RunID:   "run-1",
		AgentID: "agent-1",
		System:  "before",
	}

	hook := mutatingPromptHook{}
	if err := hook.BeforePromptBuild(context.Background(), event, NewControl()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.System != "after" {
		t.Fatalf("expected prompt event mutation to persist, got %q", event.System)
	}
}

func TestContextEvent_IsMutablySharedThroughHook(t *testing.T) {
	event := &ContextEvent{
		RunID:    "run-1",
		AgentID:  "agent-1",
		Messages: []Message{{Role: "user", Content: "before"}},
	}

	if len(event.Messages) != 1 {
		t.Fatal("expected initial message")
	}

	hook := mutatingContextHook{}
	if err := hook.BeforeContextBuild(context.Background(), event, NewControl()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(event.Messages) != 1 || event.Messages[0].Content != "after" {
		t.Fatalf("expected context event message replacement to persist, got %#v", event.Messages)
	}
}

func TestModelEvent_RequestIsMutablySharedThroughHook(t *testing.T) {
	event := &ModelEvent{
		RunID:   "run-1",
		AgentID: "agent-1",
		Request: &GenerateRequest{Model: "before"},
	}

	hook := mutatingModelHook{}
	if err := hook.BeforeModel(context.Background(), event, NewControl()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Request == nil || event.Request.Model != "mutated" {
		t.Fatalf("expected model event request mutation to persist, got %#v", event.Request)
	}
}

func TestToolEvent_CallIsMutablySharedThroughHook(t *testing.T) {
	event := &ToolEvent{
		RunID:   "run-1",
		AgentID: "agent-1",
		Call:    &ToolCall{CallID: "call-1", Name: "before"},
	}

	hook := mutatingToolHook{}
	if err := hook.BeforeTool(context.Background(), event, NewControl()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Call == nil || event.Call.Name != "mutated" {
		t.Fatalf("expected tool event call mutation to persist, got %#v", event.Call)
	}
}
