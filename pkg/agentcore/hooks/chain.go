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

func (c Chain) BeforeRun(ctx context.Context, event *agentcore.RunEvent, ctrl *agentcore.Control) error {
	for _, hook := range c.hooks {
		if err := hook.BeforeRun(ctx, event, ctrl); err != nil {
			return err
		}
	}
	return nil
}

func (c Chain) BeforePromptBuild(ctx context.Context, event *agentcore.PromptEvent, ctrl *agentcore.Control) error {
	for _, hook := range c.hooks {
		if err := hook.BeforePromptBuild(ctx, event, ctrl); err != nil {
			return err
		}
	}
	return nil
}

func (c Chain) BeforeContextBuild(ctx context.Context, event *agentcore.ContextEvent, ctrl *agentcore.Control) error {
	for _, hook := range c.hooks {
		if err := hook.BeforeContextBuild(ctx, event, ctrl); err != nil {
			return err
		}
	}
	return nil
}

func (c Chain) BeforeModel(ctx context.Context, event *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	for _, hook := range c.hooks {
		if err := hook.BeforeModel(ctx, event, ctrl); err != nil {
			return err
		}
	}
	return nil
}

func (c Chain) AfterModel(ctx context.Context, event *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	for _, hook := range c.hooks {
		if err := hook.AfterModel(ctx, event, ctrl); err != nil {
			return err
		}
	}
	return nil
}

func (c Chain) BeforeTool(ctx context.Context, event *agentcore.ToolEvent, ctrl *agentcore.Control) error {
	for _, hook := range c.hooks {
		if err := hook.BeforeTool(ctx, event, ctrl); err != nil {
			return err
		}
	}
	return nil
}

func (c Chain) AfterTool(ctx context.Context, event *agentcore.ToolEvent, ctrl *agentcore.Control) error {
	for _, hook := range c.hooks {
		if err := hook.AfterTool(ctx, event, ctrl); err != nil {
			return err
		}
	}
	return nil
}

func (c Chain) AfterRun(ctx context.Context, event *agentcore.RunEvent, ctrl *agentcore.Control) error {
	for _, hook := range c.hooks {
		if err := hook.AfterRun(ctx, event, ctrl); err != nil {
			return err
		}
	}
	return nil
}

func (c Chain) OnError(ctx context.Context, event *agentcore.ErrorEvent, ctrl *agentcore.Control) error {
	for _, hook := range c.hooks {
		if err := hook.OnError(ctx, event, ctrl); err != nil {
			return err
		}
	}
	return nil
}
