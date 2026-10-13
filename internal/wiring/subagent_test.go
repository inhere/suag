package wiring

import (
	"testing"

	"github.com/gookit/goutil/x/assert"

	appconfig "github.com/inhere/suag/internal/config"
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
