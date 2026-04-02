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
