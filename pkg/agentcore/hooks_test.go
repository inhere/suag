package agentcore

import (
	"context"
	"testing"
)

type testHook struct{}

func (testHook) BeforeRun(context.Context, RunEvent, *Control) error              { return nil }
func (testHook) BeforePromptBuild(context.Context, PromptEvent, *Control) error   { return nil }
func (testHook) BeforeContextBuild(context.Context, ContextEvent, *Control) error { return nil }
func (testHook) BeforeModel(context.Context, ModelEvent, *Control) error          { return nil }
func (testHook) AfterModel(context.Context, ModelEvent, *Control) error           { return nil }
func (testHook) BeforeTool(context.Context, ToolEvent, *Control) error            { return nil }
func (testHook) AfterTool(context.Context, ToolEvent, *Control) error             { return nil }
func (testHook) AfterRun(context.Context, RunEvent, *Control) error               { return nil }
func (testHook) OnError(context.Context, ErrorEvent, *Control) error              { return nil }

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
