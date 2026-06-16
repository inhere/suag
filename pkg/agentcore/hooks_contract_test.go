package agentcore_test

import (
	"context"
	"testing"

	"github.com/inhere/suag/pkg/agentcore"
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
