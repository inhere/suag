package hooks

import (
	"context"

	"github.com/inhere/axon/pkg/agentcore"
)

// Chain executes hooks in registration order.
type Chain struct {
	hooks []agentcore.Hook
}

// NewChain builds an ordered hook chain.
func NewChain(hooks ...agentcore.Hook) Chain {
	filtered := make([]agentcore.Hook, 0, len(hooks))
	for _, hook := range hooks {
		if hook != nil {
			filtered = append(filtered, hook)
		}
	}
	return Chain{hooks: filtered}
}

func (c Chain) runStage(call func(agentcore.Hook) error, ctrl *agentcore.Control) error {
	for _, hook := range c.hooks {
		if err := call(hook); err != nil {
			return err
		}
		if ctrl != nil && (ctrl.ShouldShortCircuit() || ctrl.ShouldYield()) {
			return nil
		}
	}
	return nil
}

func (c Chain) runStageWithoutControlStop(call func(agentcore.Hook) error) error {
	for _, hook := range c.hooks {
		if err := call(hook); err != nil {
			return err
		}
	}
	return nil
}

func (c Chain) BeforeRun(ctx context.Context, event *agentcore.RunEvent, ctrl *agentcore.Control) error {
	return c.runStage(func(hook agentcore.Hook) error {
		return hook.BeforeRun(ctx, event, ctrl)
	}, ctrl)
}

func (c Chain) BeforePromptBuild(ctx context.Context, event *agentcore.PromptEvent, ctrl *agentcore.Control) error {
	return c.runStage(func(hook agentcore.Hook) error {
		return hook.BeforePromptBuild(ctx, event, ctrl)
	}, ctrl)
}

func (c Chain) BeforeContextBuild(ctx context.Context, event *agentcore.ContextEvent, ctrl *agentcore.Control) error {
	return c.runStage(func(hook agentcore.Hook) error {
		return hook.BeforeContextBuild(ctx, event, ctrl)
	}, ctrl)
}

func (c Chain) BeforeModel(ctx context.Context, event *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	return c.runStage(func(hook agentcore.Hook) error {
		return hook.BeforeModel(ctx, event, ctrl)
	}, ctrl)
}

func (c Chain) AfterModel(ctx context.Context, event *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	return c.runStage(func(hook agentcore.Hook) error {
		return hook.AfterModel(ctx, event, ctrl)
	}, ctrl)
}

func (c Chain) BeforeTool(ctx context.Context, event *agentcore.ToolEvent, ctrl *agentcore.Control) error {
	return c.runStage(func(hook agentcore.Hook) error {
		return hook.BeforeTool(ctx, event, ctrl)
	}, ctrl)
}

func (c Chain) AfterTool(ctx context.Context, event *agentcore.ToolEvent, ctrl *agentcore.Control) error {
	return c.runStageWithoutControlStop(func(hook agentcore.Hook) error {
		return hook.AfterTool(ctx, event, ctrl)
	})
}

func (c Chain) AfterRun(ctx context.Context, event *agentcore.RunEvent, ctrl *agentcore.Control) error {
	return c.runStage(func(hook agentcore.Hook) error {
		return hook.AfterRun(ctx, event, ctrl)
	}, ctrl)
}

func (c Chain) OnError(ctx context.Context, event *agentcore.ErrorEvent, ctrl *agentcore.Control) error {
	return c.runStage(func(hook agentcore.Hook) error {
		return hook.OnError(ctx, event, ctrl)
	}, ctrl)
}

func (c Chain) OnErrorObserved(ctx context.Context, event *agentcore.ErrorEvent, ctrl *agentcore.Control) error {
	return c.runStageWithoutControlStop(func(hook agentcore.Hook) error {
		return hook.OnError(ctx, event, ctrl)
	})
}
