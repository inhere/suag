package wiring

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"

	appconfig "github.com/inhere/suga/internal/config"
	"github.com/inhere/suga/internal/personal"
)

// TestSubAgentModel: a sub-agent runs on agents.model when set, else on the
// parent's model (its --model override included), never silently on the
// config's model alone.
func TestSubAgentModel(t *testing.T) {
	workspace := manifestConfig(t) // model openai/gpt-4o
	build := func(t *testing.T, agentsModel, mainModel string) string {
		t.Helper()
		s := &subAgents{workspace: workspace, ruleCWD: workspace, mainModel: mainModel,
			settings: appconfig.AgentSettings{Model: agentsModel, MaxSteps: 5}}
		runner, closeIt, err := s.build("explore")
		assert.Require(t, assert.NoErr(t, err))
		t.Cleanup(closeIt)
		return runner.ProviderName() + "/" + runner.Model()
	}
	t.Run("the config's model by default", func(t *testing.T) {
		assert.Eq(t, "openai/gpt-4o", build(t, "", ""))
	})
	t.Run("the parent's --model", func(t *testing.T) {
		assert.Eq(t, "openai/gpt-4.1", build(t, "", "openai/gpt-4.1"))
	})
	t.Run("agents.model wins; a bare id uses the main model's provider", func(t *testing.T) {
		assert.Eq(t, "openai/gpt-4o-mini", build(t, "gpt-4o-mini", "openai/gpt-4.1"))
	})
}

func TestSubAgentBudgetProviderEstimatesUsage(t *testing.T) {
	workspace := manifestConfig(t)
	server := noUsageServer(t, 200, "child", nil)
	setProviders(t, map[string]any{"openai": map[string]any{"api_key": "k", "base_url": server.URL}})
	s := &subAgents{workspace: workspace, ruleCWD: workspace, settings: appconfig.AgentSettings{MaxSteps: 1}, budget: personal.BudgetSettings{Tokens: 100, Estimate: personal.EstimateAuto}}
	runner, closeIt, err := s.build("explore")
	assert.Require(t, assert.NoErr(t, err))
	t.Cleanup(closeIt)
	res, err := runner.Run(context.Background(), personal.RunRequest{Prompt: "child"})
	assert.NoErr(t, err)
	assert.True(t, res.Usage.InputTokens > 0)
	assert.True(t, res.Usage.OutputTokens > 0)
}
